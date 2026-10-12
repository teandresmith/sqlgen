package mysql

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strconv"
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
	// generated holds, per table, the names of the indexes MySQL built on its
	// own for a foreign key. A later key can make one redundant.
	generated map[string]map[string]bool
	// exprKeys holds, per table, its keys with an expression part. They are
	// not listed, but each keeps its name, and one whose leading columns
	// match a foreign key's serves as its index.
	exprKeys map[string][]parser.Constraint
	// inline holds, per table and column name, the inline ENUM or SET of the
	// column. Every definition of a column records its own or clears the key,
	// and RENAME COLUMN and RENAME TABLE move it, so the key of each column
	// the schema has is current. Schema derives the entries and the column
	// types from the columns the schema has, so a key a dropped column left is
	// never read.
	inline map[string]map[string]inlineType
	// seededEnums and seededSets hold the entries of a seeded schema that no
	// table column declares inline, such as an introspected view's own.
	seededEnums []parser.Enum
	seededSets  []parser.Set
}

// inlineType is the ENUM or SET a column declares inline.
type inlineType struct {
	set    bool
	values []string
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
		generated:  make(map[string]map[string]bool),
		exprKeys:   make(map[string][]parser.Constraint),
		inline:     make(map[string]map[string]inlineType),
	}, nil
}

// NormalizeExpr rewrites the SQL expression expr in the form the parser writes
// expressions, so text read from the catalog compares equal to parsed DDL.
// Redundant parentheses, backticks and charset introducers are dropped, and a
// current-time synonym takes its standard name: "(`qty` * 2)" becomes
// "qty * 2", "_utf8mb4'x'" becomes "'x'", and "now(3)" becomes
// "current_timestamp(3)".
func NormalizeExpr(expr string) (string, error) {
	vp, err := sqlparser.New(sqlparser.Options{})
	if err != nil {
		return "", fmt.Errorf("creating mysql parser: %w", err)
	}
	e, err := vp.ParseExpr(expr)
	if err != nil {
		return "", fmt.Errorf("parsing expression %q: %w", expr, err)
	}
	return exprText(e), nil
}

// exprText writes e without charset introducers and with each synonym of a
// current-time function under its standard name. MySQL's catalog adds the
// connection charset's introducer to every string literal in a stored
// expression, and respells the synonyms: DEFAULT NOW() reads back as
// CURRENT_TIMESTAMP, DEFAULT (CURRENT_DATE) as curdate(). So neither source
// keeps the DDL's spelling.
func exprText(e sqlparser.Expr) string {
	e = sqlparser.Rewrite(sqlparser.CloneExpr(e), nil, func(c *sqlparser.Cursor) bool {
		switch n := c.Node().(type) {
		case *sqlparser.IntroducerExpr:
			c.Replace(n.Expr)
		case *sqlparser.CurTimeFuncExpr:
			if name, ok := currentTimeSynonyms[n.Name.Lowered()]; ok {
				c.Replace(&sqlparser.CurTimeFuncExpr{Name: sqlparser.NewIdentifierCI(name), Fsp: n.Fsp})
			}
		case *sqlparser.FuncExpr:
			if n.Qualifier.IsEmpty() && n.Name.Lowered() == "curdate" && len(n.Exprs) == 0 {
				c.Replace(&sqlparser.FuncExpr{Name: sqlparser.NewIdentifierCI("current_date")})
			}
		}
		return true
	}).(sqlparser.Expr)
	return sqlparser.String(e)
}

// currentTimeSynonyms maps each MySQL synonym of a current-time function to
// the standard name exprText writes.
var currentTimeSynonyms = map[string]string{
	"now":            "current_timestamp",
	"localtime":      "current_timestamp",
	"localtimestamp": "current_timestamp",
	"curtime":        "current_time",
}

// QuoteLiteral writes value as a string literal in the form the parser
// writes one: single-quoted, with MySQL backslash escapes.
func QuoteLiteral(value string) string {
	return sqlparser.String(sqlparser.NewStrLiteral(value))
}

// zerofillWidths is the display width MySQL gives a ZEROFILL integer type
// declared without one.
var zerofillWidths = map[string]int{
	"tinyint": 3, "smallint": 5, "mediumint": 8, "int": 10, "bigint": 20,
}

// typeSynonyms maps a MySQL type synonym to the type the catalog stores it as.
// BOOL and BOOLEAN are tinyint(1), handled in NormalizeType.
var typeSynonyms = map[string]string{
	"integer": "int",
	"numeric": "decimal",
	"real":    "double",
	"float4":  "float",
	"float8":  "double",
}

// maxFloatPrecision is the largest FLOAT(p) precision MySQL stores as float;
// a larger one is double.
const maxFloatPrecision = 24

// NormalizeType writes a column type as MySQL 8.0.19+ stores it in
// INFORMATION_SCHEMA.COLUMNS.COLUMN_TYPE, so the DDL and the catalog agree:
// lowercased, a synonym in its stored form, an integer type without its display
// width except tinyint(1) and a ZEROFILL type, and ZEROFILL implying UNSIGNED.
// So "INT(11)" becomes "int", "int zerofill" "int(10) unsigned zerofill",
// "BOOLEAN" "tinyint(1)" and "NUMERIC(8,3)" "decimal(8,3)".
func NormalizeType(columnType string) string {
	typ := strings.ToLower(strings.TrimSpace(columnType))
	t := splitType(typ)
	if t.base == "bool" || t.base == "boolean" {
		return "tinyint(1)"
	}
	if canonical, ok := typeSynonyms[t.base]; ok {
		t.base = canonical
	}
	if defaultWidth, isInteger := zerofillWidths[t.base]; isInteger {
		return normalizeInteger(t, defaultWidth)
	}

	switch t.base {
	case "decimal", "float", "double":
		return normalizeNonInteger(t)
	case "char", "binary", "bit":
		if t.args == "" {
			t.args = "1"
		}
		return t.base + "(" + t.args + ")" + t.rest
	case "year":
		return t.base + t.rest
	default:
		return typ
	}
}

// typeParts is a lowercased column type split into its base, the text inside
// its parentheses, and its attributes.
type typeParts struct {
	base, args string
	// rest is the text after the base and its parentheses.
	rest               string
	unsigned, zerofill bool
}

func splitType(typ string) typeParts {
	t := typeParts{base: typ}
	if i := strings.IndexAny(typ, "( "); i >= 0 {
		t.base, t.rest = typ[:i], typ[i:]
	}
	if strings.HasPrefix(t.rest, "(") {
		if inner, after, ok := strings.Cut(t.rest[1:], ")"); ok {
			t.args, t.rest = inner, after
		}
	}
	attrs := strings.Fields(t.rest)
	t.zerofill = slices.Contains(attrs, "zerofill")
	t.unsigned = t.zerofill || slices.Contains(attrs, "unsigned")
	return t
}

// String writes a numeric type: base, (args), then unsigned and zerofill.
func (t typeParts) String() string {
	s := t.base
	if t.args != "" {
		s += "(" + t.args + ")"
	}
	if t.unsigned {
		s += " unsigned"
	}
	if t.zerofill {
		s += " zerofill"
	}
	return s
}

// normalizeNonInteger writes decimal with its precision and scale, and
// float(p) as float or double. double, and float(M,D), keep their (M,D).
func normalizeNonInteger(t typeParts) string {
	switch t.base {
	case "decimal":
		switch {
		case t.args == "":
			t.args = "10,0"
		case !strings.Contains(t.args, ","):
			t.args += ",0"
		}
	case "float":
		if p, err := strconv.Atoi(t.args); err == nil {
			if p > maxFloatPrecision {
				t.base = "double"
			}
			t.args = ""
		}
	}
	return t.String()
}

// normalizeInteger drops an integer type's display width, except tinyint(1)'s
// and a ZEROFILL type's, which takes defaultWidth when it has none.
func normalizeInteger(t typeParts, defaultWidth int) string {
	switch {
	case t.zerofill:
		if t.args == "" {
			t.args = strconv.Itoa(defaultWidth)
		}
	case t.base == "tinyint" && t.args == "1" && !t.unsigned:
	default:
		t.args = ""
	}
	return t.String()
}

// Seed pre-populates the parser with an existing schema. This enables "both"
// mode where introspected tables are loaded before file-based DDL is applied.
//
// A column typed with the name its table and column derive
// (parser.InlineEnumName, parser.InlineSetName) takes the first entry of that
// name not yet taken, in table and column order, the order the introspector
// adds them in, so DDL that renames or drops it updates its entry.
func (p *Parser) Seed(base *parser.Schema) {
	p.schema = base
	p.tableIndex = make(map[string]int, len(base.Tables))
	p.inline = make(map[string]map[string]inlineType)
	enums, sets := slices.Clone(base.Enums), slices.Clone(base.Sets)
	for i, t := range base.Tables {
		p.tableIndex[t.Name] = i
		for _, c := range t.Columns {
			if c.Type == parser.InlineEnumName(t.Name, c.Name) {
				if j := slices.IndexFunc(enums, func(e parser.Enum) bool { return e.Name == c.Type }); j >= 0 {
					p.setInline(t.Name, c.Name, &inlineType{values: enums[j].Values})
					enums = slices.Delete(enums, j, j+1)
				}
			}
			if c.Type == parser.InlineSetName(t.Name, c.Name) {
				if j := slices.IndexFunc(sets, func(s parser.Set) bool { return s.Name == c.Type }); j >= 0 {
					p.setInline(t.Name, c.Name, &inlineType{set: true, values: sets[j].Values})
					sets = slices.Delete(sets, j, j+1)
				}
			}
		}
	}
	p.seededEnums, p.seededSets = enums, sets
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
		if err := p.processStmt(stmt, piece); err != nil {
			return fmt.Errorf("parsing %s: %w", filename, err)
		}
	}

	return nil
}

// Schema returns the accumulated schema model. Each inline ENUM or SET column
// is typed with, and registers an entry under, the name built from its current
// table and column names, as the introspector names it from the catalog.
func (p *Parser) Schema() *parser.Schema {
	enums, sets := slices.Clone(p.seededEnums), slices.Clone(p.seededSets)
	for i := range p.schema.Tables {
		t := &p.schema.Tables[i]
		for j := range t.Columns {
			c := &t.Columns[j]
			it, ok := p.inline[t.Name][c.Name]
			switch {
			case !ok:
			case it.set:
				c.Type = parser.InlineSetName(t.Name, c.Name)
				sets = append(sets, parser.Set{Name: c.Type, Values: slices.Clone(it.values)})
			default:
				c.Type = parser.InlineEnumName(t.Name, c.Name)
				enums = append(enums, parser.Enum{Name: c.Type, Values: slices.Clone(it.values)})
			}
		}
	}
	p.schema.Enums, p.schema.Sets = enums, sets
	return p.schema
}

// setInline records it as the inline ENUM or SET of the column, or records
// that the column has none when it is nil.
func (p *Parser) setInline(table, column string, it *inlineType) {
	if it == nil {
		delete(p.inline[table], column)
		return
	}
	if p.inline[table] == nil {
		p.inline[table] = make(map[string]inlineType)
	}
	p.inline[table][column] = *it
}

// renameInline gives column newName of table the inline ENUM or SET that
// column oldName had, or none.
func (p *Parser) renameInline(table, oldName, newName string) {
	if it, ok := p.inline[table][oldName]; ok {
		p.setInline(table, newName, &it)
		return
	}
	p.setInline(table, newName, nil)
}

// processStmt applies stmt, parsed from the statement text sql.
func (p *Parser) processStmt(stmt sqlparser.Statement, sql string) error {
	switch s := stmt.(type) {
	case *sqlparser.CreateTable:
		return p.parseCreateTable(s, sql)
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
// Unlike ALTER TABLE a RENAME TO a, which MySQL accepts as a no-op, RENAME
// TABLE a TO a is an error (ERROR 1050).
func (p *Parser) parseRenameTable(stmt *sqlparser.RenameTable) error {
	for _, pair := range stmt.TablePairs {
		from, to := pair.FromTable.Name.String(), pair.ToTable.Name.String()
		if _, exists := p.tableIndex[from]; exists && from == to {
			return fmt.Errorf("table %s already exists", to)
		}
		if err := p.renameTable(from, to); err != nil {
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
		if err := parser.CheckTableDropRefs(p.schema.Tables, "mysql", "", name); err != nil {
			return fmt.Errorf("drop table %s: %w", name, err)
		}
		p.schema.Tables = slices.Delete(p.schema.Tables, idx, idx+1)
		delete(p.tableIndex, name)
		delete(p.generated, name)
		delete(p.exprKeys, name)
		for k, v := range p.tableIndex {
			if v > idx {
				p.tableIndex[k] = v - 1
			}
		}
	}
	return nil
}

// --- CREATE TABLE ---

func (p *Parser) parseCreateTable(stmt *sqlparser.CreateTable, sql string) error {
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

	elements, err := p.tableElements(sql, stmt.TableSpec)
	if err != nil {
		return fmt.Errorf("create table %s: %w", tableName, err)
	}
	keys, others := p.parseTableSpec(&table, stmt.TableSpec, elements)
	p.generated[tableName] = make(map[string]bool)
	for i, key := range keys {
		if key.built && replacedLater(key, keys[i+1:]) {
			continue
		}
		p.addKey(&table, key.Constraint, key.built)
	}
	nameCreatedConstraints(&table, others)
	parser.CanonicalizeConstraints(&table, "mysql")

	p.tableIndex[tableName] = len(p.schema.Tables)
	p.schema.Tables = append(p.schema.Tables, table)

	return nil
}

// tableKey is a key of a new table: a declared key, or an index MySQL builds
// for a foreign key.
type tableKey struct {
	parser.Constraint
	built bool
}

// parseTableSpec adds spec's columns to table and returns its keys, with the
// index MySQL builds for each foreign key, and its FOREIGN KEY and CHECK
// constraints. Both come in declaration order, which elements gives, since
// MySQL numbers unnamed keys and unnamed constraints in that order.
func (p *Parser) parseTableSpec(table *parser.Table, spec *sqlparser.TableSpec, elements []tableElement) (keys []tableKey, others []parser.Constraint) {
	var col, idx, con int
	for _, element := range elements {
		switch element {
		case columnElement:
			c, constraints := p.parseColumn(table.Name, spec.Columns[col])
			col++
			table.Columns = append(table.Columns, c)
			for _, constraint := range constraints {
				if isKey(constraint.Type) {
					keys = append(keys, tableKey{Constraint: constraint})
					continue
				}
				others = append(others, constraint)
				keys = append(keys, tableKey{Constraint: foreignKeyIndex(constraint, ""), built: true})
			}
		case indexElement:
			keys = append(keys, tableKey{Constraint: p.parseIndex(spec.Indexes[idx])})
			idx++
		case constraintElement:
			cd := spec.Constraints[con]
			con++
			constraint := p.parseConstraintDef(cd)
			others = append(others, constraint)
			if fk, ok := cd.Details.(*sqlparser.ForeignKeyDefinition); ok {
				keys = append(keys, tableKey{Constraint: foreignKeyIndex(constraint, fk.IndexName.String()), built: true})
			}
		}
	}
	return keys, others
}

// replacedLater reports whether a key in later, the keys declared after the
// index MySQL would build for a foreign key, makes that index redundant. MySQL
// then builds no index, so it takes no name ahead of the keys between.
func replacedLater(key tableKey, later []tableKey) bool {
	return slices.ContainsFunc(later, func(k tableKey) bool {
		if k.built && len(k.Columns) < len(key.Columns) {
			return false
		}
		return redundantKeys(key.Columns, true, k.Columns, k.built)
	})
}

// tableElement is the kind of one entry of a CREATE TABLE definition list, as
// the vitess TableSpec files it: a column, an index (PRIMARY KEY, UNIQUE,
// KEY / INDEX, FULLTEXT, SPATIAL) or a constraint (FOREIGN KEY, CHECK).
type tableElement int

const (
	columnElement tableElement = iota
	indexElement
	constraintElement
)

// tableElements returns the entries of the definition list of the CREATE
// TABLE statement sql in declaration order, which the TableSpec spec parsed
// from it does not keep: it holds columns, indexes and constraints in separate
// lists. A column declared with a CHECK yields the column and then the
// constraint, since vitess files that CHECK among the constraints. It reports
// an error when the entries it reads do not match spec.
func (p *Parser) tableElements(sql string, spec *sqlparser.TableSpec) ([]tableElement, error) {
	var elements []tableElement
	var tokens []int // the current entry's tokens outside nested parentheses
	depth := 0
	tkn := p.vparser.NewStringTokenizer(sql)
scan:
	for {
		typ, _ := tkn.Scan()
		switch {
		case typ == 0 || typ == sqlparser.LEX_ERROR:
			break scan
		case typ == sqlparser.COMMENT:
			continue
		case typ == '(':
			depth++
			if depth == 1 {
				continue
			}
		case typ == ')':
			depth--
			if depth == 0 {
				elements = append(elements, classifyTableElement(tokens)...)
				break scan
			}
		case typ == ',' && depth == 1:
			elements = append(elements, classifyTableElement(tokens)...)
			tokens = tokens[:0]
			continue
		}
		if depth == 1 {
			tokens = append(tokens, typ)
		}
	}
	if err := matchTableSpec(elements, spec); err != nil {
		return nil, err
	}
	return elements, nil
}

// matchTableSpec reports an error when elements does not hold as many
// columns, indexes and constraints as spec.
func matchTableSpec(elements []tableElement, spec *sqlparser.TableSpec) error {
	var counts [3]int
	for _, e := range elements {
		counts[e]++
	}
	if counts != [3]int{len(spec.Columns), len(spec.Indexes), len(spec.Constraints)} {
		return fmt.Errorf("cannot recover declaration order: read %d columns, %d indexes and %d constraints, parsed %d, %d and %d",
			counts[columnElement], counts[indexElement], counts[constraintElement], len(spec.Columns), len(spec.Indexes), len(spec.Constraints))
	}
	return nil
}

// classifyTableElement returns what the definition-list entry with the
// top-level tokens tokens is filed as. The keywords it looks for are reserved
// in MySQL, so a column cannot start with one unquoted.
func classifyTableElement(tokens []int) []tableElement {
	if len(tokens) == 0 {
		return nil
	}
	switch tokens[0] {
	case sqlparser.PRIMARY, sqlparser.UNIQUE, sqlparser.KEY, sqlparser.INDEX, sqlparser.FULLTEXT, sqlparser.SPATIAL:
		return []tableElement{indexElement}
	case sqlparser.FOREIGN, sqlparser.CHECK:
		return []tableElement{constraintElement}
	case sqlparser.CONSTRAINT:
		for _, typ := range tokens[1:] {
			switch typ {
			case sqlparser.PRIMARY, sqlparser.UNIQUE:
				return []tableElement{indexElement}
			case sqlparser.FOREIGN, sqlparser.CHECK:
				return []tableElement{constraintElement}
			}
		}
	}
	if slices.Contains(tokens, sqlparser.CHECK) {
		return []tableElement{columnElement, constraintElement}
	}
	return []tableElement{columnElement}
}

// nameCreatedConstraints adds the FOREIGN KEY and CHECK constraints of a new
// table. An unnamed one is numbered among the unnamed ones of its kind in the
// statement, as MySQL's CREATE TABLE numbers them.
func nameCreatedConstraints(table *parser.Table, constraints []parser.Constraint) {
	var fks, checks []string
	for _, c := range constraints {
		c.Columns = declaredColumns(table, c.Columns)
		if c.Name == "" {
			generated := &fks
			if c.Type == parser.Check {
				generated = &checks
			}
			c.Name = parser.DeriveMySQLConstraintName(table.Name, c, *generated)
			*generated = append(*generated, c.Name)
		}
		table.Constraints = append(table.Constraints, c)
	}
}

// parseColumn returns the column col defines and the keys and foreign key
// declared inline on it, as table constraints over the column.
func (p *Parser) parseColumn(tableName string, col *sqlparser.ColumnDefinition) (parser.Column, []parser.Constraint) {
	c := parser.Column{
		Name:     col.Name.String(),
		Nullable: true,
		Type:     buildTypeString(col.Type),
	}

	opts := col.Type.Options
	if opts == nil {
		return c, nil
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

	// Default value. MySQL stores DEFAULT NULL as no default at all, so it is
	// recorded as none.
	if _, isNull := opts.Default.(*sqlparser.NullVal); opts.Default != nil && !isNull {
		c.Default = exprText(opts.Default)
	}

	// Generated column: GENERATED ALWAYS AS (expr) or the shorthand AS (expr).
	// Storage is VIRTUAL unless declared STORED.
	if opts.As != nil {
		c.GeneratedExpr = exprText(opts.As)
		c.GeneratedStorage = "VIRTUAL"
		if opts.Storage == sqlparser.StoredStorage {
			c.GeneratedStorage = "STORED"
		}
	}

	// Column comment.
	if opts.Comment != nil {
		c.Comment = opts.Comment.Val
	}

	// Column-level key options.
	var constraints []parser.Constraint
	switch opts.KeyOpt {
	case sqlparser.ColKeyPrimary:
		c.Nullable = false
		constraints = append(constraints, parser.Constraint{Name: "PRIMARY", Type: parser.PrimaryKey, Columns: []string{c.Name}})
	case sqlparser.ColKeyUnique, sqlparser.ColKeyUniqueKey:
		constraints = append(constraints, parser.Constraint{Type: parser.Unique, Columns: []string{c.Name}})
	case sqlparser.ColKeyNone, sqlparser.ColKeySpatialKey, sqlparser.ColKeyFulltextKey, sqlparser.ColKey:
		// No key for these key options.
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
		constraints = append(constraints, parser.Constraint{
			Type:             parser.ForeignKey,
			Columns:          []string{c.Name},
			ReferenceTable:   ref.ReferencedTable.Name.String(),
			ReferenceColumns: identifiers(ref.ReferencedColumns),
		})
	}

	// Inline ENUM/SET — extract values and create schema entries.
	p.extractInlineEnumOrSet(tableName, &c, col.Type)

	return c, constraints
}

// extractInlineEnumOrSet records the inline ENUM or SET that column c of
// table tableName declares, or that it declares none, in place of what an
// earlier definition of the column recorded, and types c with the
// table-scoped name (parser.InlineEnumName, parser.InlineSetName). Schema
// registers the entries.
func (p *Parser) extractInlineEnumOrSet(tableName string, c *parser.Column, ct *sqlparser.ColumnType) {
	typ := strings.ToLower(ct.Type)
	if len(ct.EnumValues) == 0 || (typ != "enum" && typ != "set") {
		p.setInline(tableName, c.Name, nil)
		return
	}
	values := make([]string, len(ct.EnumValues))
	for i, v := range ct.EnumValues {
		values[i] = decodeEnumValue(v)
	}
	if typ == "set" {
		c.Type = parser.InlineSetName(tableName, c.Name)
	} else {
		c.Type = parser.InlineEnumName(tableName, c.Name)
	}
	p.setInline(tableName, c.Name, &inlineType{set: typ == "set", values: values})
}

// decodeEnumValue returns the value of one sqlparser EnumValues entry. The
// parser re-encodes each value as a quoted literal with MySQL backslash
// escapes ('it\'s', 'b\\c'), so the value is the literal's contents with
// those escapes undone.
func decodeEnumValue(lit string) string {
	if len(lit) >= 2 && lit[0] == '\'' && lit[len(lit)-1] == '\'' {
		lit = lit[1 : len(lit)-1]
	}
	if !strings.Contains(lit, "\\") {
		return lit
	}
	var b strings.Builder
	for i := 0; i < len(lit); i++ {
		if lit[i] == '\\' && i+1 < len(lit) {
			i++
			b.WriteString(unescapeMySQL(lit[i]))
			continue
		}
		b.WriteByte(lit[i])
	}
	return b.String()
}

// unescapeMySQL returns what the escape sequence \c stands for in a MySQL
// string literal. \% and \_ keep their backslash, as MySQL does.
func unescapeMySQL(c byte) string {
	switch c {
	case '0':
		return "\x00"
	case 'b':
		return "\b"
	case 'n':
		return "\n"
	case 'r':
		return "\r"
	case 't':
		return "\t"
	case 'Z':
		return "\x1a"
	case '%', '_':
		return "\\" + string(c)
	default:
		return string(c)
	}
}

// buildTypeString builds the SQL type string from a ColumnType, in the form
// NormalizeType gives the catalog's.
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

	if ct.Unsigned {
		sb.WriteString(" unsigned")
	}
	if ct.Zerofill {
		sb.WriteString(" zerofill")
	}

	return NormalizeType(sb.String())
}

func (p *Parser) parseIndex(idx *sqlparser.IndexDefinition) parser.Constraint {
	cols := indexColumns(idx)
	name := idx.Info.Name.String()

	switch idx.Info.Type {
	case sqlparser.IndexTypePrimary:
		// MySQL names a primary key PRIMARY, whatever name it is declared with.
		return parser.Constraint{
			Name:    "PRIMARY",
			Type:    parser.PrimaryKey,
			Columns: cols,
		}
	case sqlparser.IndexTypeUnique:
		// With no index name, MySQL names the key after its CONSTRAINT symbol.
		if name == "" {
			name = idx.Info.ConstraintName.String()
		}
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
		var refTable string
		var refCols []string
		if d.ReferenceDefinition != nil {
			refTable = d.ReferenceDefinition.ReferencedTable.Name.String()
			refCols = identifiers(d.ReferenceDefinition.ReferencedColumns)
		}
		return parser.Constraint{
			Name:             name,
			Type:             parser.ForeignKey,
			Columns:          identifiers(d.Source),
			ReferenceTable:   refTable,
			ReferenceColumns: refCols,
		}

	case *sqlparser.CheckConstraintDefinition:
		return parser.Constraint{
			Name:            name,
			Type:            parser.Check,
			CheckExpression: exprText(d.Expr),
		}

	default:
		return parser.Constraint{Name: name}
	}
}

// foreignKeyIndex returns the index MySQL builds for the foreign key fk when
// no other index starts with its columns. It is named after the constraint's
// declared name, else the index name declared in FOREIGN KEY <name> (…), else
// it takes the name of its first column.
func foreignKeyIndex(fk parser.Constraint, indexName string) parser.Constraint {
	return parser.Constraint{
		Name:    cmp.Or(fk.Name, indexName),
		Type:    parser.Index,
		Columns: slices.Clone(fk.Columns),
	}
}

func isKey(t parser.ConstraintType) bool {
	return t == parser.PrimaryKey || t == parser.Unique || t == parser.Index
}

// addKey adds a key to table as MySQL adds a key after the ones it already
// has. When one of the two is an index MySQL built for a foreign key and its
// columns start the other's, one is redundant: a built index gives way to a
// declared key, and of two built indexes the shorter goes, or the earlier
// when they match. A PRIMARY KEY is named PRIMARY; any other unnamed key takes
// its first column's name, made unique among the keys the table has. A key
// with an expression part is not a key over columns (PRD §30.7): it is kept
// in exprKeys, not listed, and an unnamed one is named functional_index.
func (p *Parser) addKey(table *parser.Table, key parser.Constraint, generated bool) {
	key.Columns = declaredColumns(table, key.Columns)
	built := p.generated[table.Name]
	if built == nil {
		built = make(map[string]bool)
		p.generated[table.Name] = built
	}
	exprKeys := p.exprKeys[table.Name]
	if generated && slices.ContainsFunc(exprKeys, func(k parser.Constraint) bool {
		return columnsPrefixMatch(k.Columns, key.Columns)
	}) {
		return
	}
	for i, k := range table.Constraints {
		if !isKey(k.Type) || !redundantKeys(key.Columns, generated, k.Columns, built[k.Name]) {
			continue
		}
		if !built[k.Name] || (generated && len(key.Columns) < len(k.Columns)) {
			return
		}
		table.Constraints = slices.Delete(table.Constraints, i, i+1)
		delete(built, k.Name)
		break
	}
	expr := slices.Contains(key.Columns, "")
	if key.Name == "" {
		names := constraintNames(table, key.Type)
		for _, k := range exprKeys {
			names = append(names, k.Name)
		}
		named := key
		if expr {
			named.Columns = []string{"functional_index"}
		}
		key.Name = parser.DeriveMySQLConstraintName(table.Name, named, names)
	}
	if expr {
		p.exprKeys[table.Name] = append(exprKeys, key)
		return
	}
	table.Constraints = append(table.Constraints, key)
	if generated {
		built[key.Name] = true
	}
}

// redundantKeys is MySQL's foreign_key_prefix test: whether, of keys a and b,
// one is an index built for a foreign key (the shorter one, when both are)
// whose columns start the other's.
func redundantKeys(a []string, aBuilt bool, b []string, bBuilt bool) bool {
	switch {
	case aBuilt && bBuilt:
		if len(a) > len(b) {
			a, b = b, a
		}
	case bBuilt:
		a, b = b, a
	case !aBuilt:
		return false
	}
	return columnsPrefixMatch(b, a)
}

// columnsPrefixMatch reports whether prefix equals the leading columns of
// indexCols in order.
func columnsPrefixMatch(indexCols, prefix []string) bool {
	if len(prefix) == 0 || len(prefix) > len(indexCols) {
		return false
	}
	for i, col := range prefix {
		if !strings.EqualFold(indexCols[i], col) {
			return false
		}
	}
	return true
}

// addConstraints adds the constraints ALTER TABLE creates, in order, each
// named as MySQL names it against what the table already has. A FOREIGN KEY
// brings the index MySQL builds for it; indexName is the index name its
// FOREIGN KEY clause declares.
func (p *Parser) addConstraints(table *parser.Table, constraints []parser.Constraint, indexName string) {
	for _, c := range constraints {
		if isKey(c.Type) {
			p.addKey(table, c, false)
			continue
		}
		named := c
		named.Columns = declaredColumns(table, c.Columns)
		if named.Name == "" {
			named.Name = parser.DeriveMySQLConstraintName(table.Name, named, constraintNames(table, c.Type))
		}
		table.Constraints = append(table.Constraints, named)
		if c.Type == parser.ForeignKey {
			p.addKey(table, foreignKeyIndex(c, indexName), true)
		}
	}
	parser.CanonicalizeConstraints(table, "mysql")
}

// constraintNames returns the names MySQL compares a new constraint of kind
// against: every key name for a key, every FOREIGN KEY or CHECK name otherwise.
func constraintNames(table *parser.Table, kind parser.ConstraintType) []string {
	var names []string
	for _, c := range table.Constraints {
		if c.Type == kind || (isKey(kind) && isKey(c.Type)) {
			names = append(names, c.Name)
		}
	}
	return names
}

// declaredColumns returns cols with each name spelled as the table declares
// the column, since MySQL column names are case-insensitive.
func declaredColumns(table *parser.Table, cols []string) []string {
	out := slices.Clone(cols)
	for i, name := range out {
		out[i] = declaredColumn(table, name)
	}
	return out
}

// declaredColumn returns name spelled as the table declares the column, or
// name itself when the table has no such column.
func declaredColumn(table *parser.Table, name string) string {
	for _, col := range table.Columns {
		if strings.EqualFold(col.Name, name) {
			return col.Name
		}
	}
	return name
}

func identifiers(cols sqlparser.Columns) []string {
	names := make([]string, len(cols))
	for i, col := range cols {
		names[i] = col.String()
	}
	return names
}

// --- ALTER TABLE ---

func (p *Parser) parseAlterTable(stmt *sqlparser.AlterTable) error {
	tableName := stmt.Table.Name.String()
	table, ok := p.lookupTable(tableName)
	if !ok {
		return fmt.Errorf("table %s not found", tableName)
	}

	// MySQL drops the old primary key before it adds a new one, wherever the
	// primary-key drop sits among the clauses, and the drop clears the key flag on
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

// dropPrimaryKey returns opt as a DropKey, and true, when it drops the primary
// key.
func dropPrimaryKey(opt sqlparser.AlterOption) (*sqlparser.DropKey, bool) {
	o, ok := opt.(*sqlparser.DropKey)
	if !ok || dropKeyType(o) != sqlparser.PrimaryKeyType {
		return nil, false
	}
	return o, true
}

// dropKeyType returns the kind of key o drops. vitess parses the ALTER TABLE
// clause DROP INDEX or DROP KEY `PRIMARY` as a normal-key drop, but a MySQL
// primary key is always named PRIMARY, and index names compare
// case-insensitively, so MySQL drops the primary key.
func dropKeyType(o *sqlparser.DropKey) sqlparser.DropKeyType {
	if o.Type == sqlparser.NormalKeyType && strings.EqualFold(o.Name.String(), "PRIMARY") {
		return sqlparser.PrimaryKeyType
	}
	return o.Type
}

func (p *Parser) applyAlterOption(table *parser.Table, opt sqlparser.AlterOption) error {
	switch o := opt.(type) {
	case *sqlparser.AddColumns:
		for _, col := range o.Columns {
			c, constraints := p.parseColumn(table.Name, col)
			table.Columns = append(table.Columns, c)
			p.addConstraints(table, constraints, "")
		}
	case *sqlparser.DropColumn:
		name := declaredColumn(table, o.Name.Name.String())
		alterDropColumn(table, name)
		p.dropExprKeyColumn(table.Name, name)
	case *sqlparser.ModifyColumn:
		return p.alterModifyColumn(table, o)
	case *sqlparser.AddIndexDefinition:
		p.addConstraints(table, []parser.Constraint{p.parseIndex(o.IndexDefinition)}, "")
	case *sqlparser.DropKey:
		applyDropKey(table, o)
		maps.DeleteFunc(p.generated[table.Name], func(name string, _ bool) bool {
			return strings.EqualFold(name, o.Name.String())
		})
		p.exprKeys[table.Name] = slices.DeleteFunc(p.exprKeys[table.Name], func(k parser.Constraint) bool {
			return strings.EqualFold(k.Name, o.Name.String())
		})
	case *sqlparser.AddConstraintDefinition:
		var indexName string
		if fk, ok := o.ConstraintDefinition.Details.(*sqlparser.ForeignKeyDefinition); ok {
			indexName = fk.IndexName.String()
		}
		p.addConstraints(table, []parser.Constraint{p.parseConstraintDef(o.ConstraintDefinition)}, indexName)
	case *sqlparser.RenameColumn:
		oldName, newName := declaredColumn(table, o.OldName.Name.String()), o.NewName.Name.String()
		if err := parser.RenameColumn(table, "mysql", oldName, newName); err != nil {
			return fmt.Errorf("rename column %s: %w", table.Name, err)
		}
		parser.RenameColumnReferences(p.schema.Tables, "mysql", table.Schema, table.Name, oldName, newName)
		p.renameExprKeyColumn(table.Name, oldName, newName)
		p.renameInline(table.Name, oldName, newName)
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
	if p.replaceColumn(table, o.NewColDefinition.Name.String(), o.NewColDefinition) {
		return nil
	}
	// Column not found — add it.
	newCol, constraints := p.parseColumn(table.Name, o.NewColDefinition)
	table.Columns = append(table.Columns, newCol)
	p.addConstraints(table, constraints, "")
	return nil
}

// alterDropColumn drops the column and takes it out of every key, as MySQL
// does. A key left with no column is dropped.
func alterDropColumn(table *parser.Table, colName string) {
	table.Columns = slices.DeleteFunc(table.Columns, func(c parser.Column) bool {
		return c.Name == colName
	})
	for i := range table.Constraints {
		if c := &table.Constraints[i]; isKey(c.Type) {
			c.Columns = slices.DeleteFunc(c.Columns, func(name string) bool { return name == colName })
		}
	}
	table.Constraints = slices.DeleteFunc(table.Constraints, func(c parser.Constraint) bool {
		return isKey(c.Type) && len(c.Columns) == 0
	})
	parser.CanonicalizeConstraints(table, "mysql")
}

func (p *Parser) alterChangeColumn(table *parser.Table, o *sqlparser.ChangeColumn) error {
	if o.NewColDefinition == nil {
		return nil
	}
	oldName := o.OldColumn.Name.String()
	if !p.replaceColumn(table, oldName, o.NewColDefinition) {
		return fmt.Errorf("column %s not found in table %s", oldName, table.Name)
	}
	return nil
}

// replaceColumn redefines the column named name, matched case-insensitively as
// MySQL does, as def. The column takes def's spelling, so MODIFY COLUMN naming
// it in another case renames it, and references follow. It reports whether the
// table has the column.
func (p *Parser) replaceColumn(table *parser.Table, name string, def *sqlparser.ColumnDefinition) bool {
	oldName := declaredColumn(table, name)
	i := slices.IndexFunc(table.Columns, func(c parser.Column) bool { return c.Name == oldName })
	if i < 0 {
		return false
	}
	newCol, constraints := p.parseColumn(table.Name, def)
	table.Columns[i] = redefineColumn(table.Columns[i], newCol)
	parser.RenameColumnReferences(p.schema.Tables, "mysql", table.Schema, table.Name, oldName, newCol.Name)
	p.renameExprKeyColumn(table.Name, oldName, newCol.Name)
	p.addConstraints(table, constraints, "")
	return true
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
	if newCol.FKReference == nil {
		newCol.FKReference = old.FKReference
	}
	return newCol
}

func applyDropKey(table *parser.Table, o *sqlparser.DropKey) {
	name := o.Name.String()
	switch dropKeyType(o) {
	case sqlparser.PrimaryKeyType:
		// A table has one primary key, so it matches by type alone. Nullability
		// stays: MySQL leaves the former key columns NOT NULL.
		removeConstraintFromColumns(table, parser.PrimaryKey, "")
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
	// Table names compare exactly (lower_case_table_names=0). Renaming a
	// table to its own name is left to the caller.
	if _, exists := p.tableIndex[newName]; exists && newName != oldName {
		return fmt.Errorf("table %s already exists", newName)
	}
	// Keys referencing the table follow it to its new name, as they do in the
	// database; left on the old name, relationship detection drops the edge.
	parser.RenameTableReferences(p.schema.Tables, "mysql", p.schema.Tables[idx].Schema, oldName, newName)
	// MySQL renames a <table>_ibfk_<n> or <table>_chk_<n> name with its table.
	for i := range p.schema.Tables[idx].Constraints {
		c := &p.schema.Tables[idx].Constraints[i]
		if c.Type == parser.ForeignKey || c.Type == parser.Check {
			c.Name = parser.RenameMySQLGeneratedName(c.Name, oldName, newName)
		}
	}
	p.schema.Tables[idx].Name = newName
	// Delete before setting, so renaming a table to its own name keeps them.
	built, exprKeys, inline := p.generated[oldName], p.exprKeys[oldName], p.inline[oldName]
	delete(p.generated, oldName)
	delete(p.exprKeys, oldName)
	delete(p.inline, oldName)
	p.generated[newName] = built
	p.exprKeys[newName] = exprKeys
	p.inline[newName] = inline
	delete(p.tableIndex, oldName)
	p.tableIndex[newName] = idx
	return nil
}

// renameExprKeyColumn renames a column in the table's expression keys, as
// MySQL renames it in every key.
func (p *Parser) renameExprKeyColumn(table, oldName, newName string) {
	for _, k := range p.exprKeys[table] {
		for i, name := range k.Columns {
			if name == oldName {
				k.Columns[i] = newName
			}
		}
	}
}

// dropExprKeyColumn takes a dropped column out of the table's expression
// keys, as MySQL takes it out of every key.
func (p *Parser) dropExprKeyColumn(table, colName string) {
	for i := range p.exprKeys[table] {
		k := &p.exprKeys[table][i]
		k.Columns = slices.DeleteFunc(k.Columns, func(name string) bool { return name == colName })
	}
}

func (p *Parser) lookupTable(name string) (*parser.Table, bool) {
	idx, ok := p.tableIndex[name]
	if !ok {
		return nil, false
	}
	return &p.schema.Tables[idx], true
}

// indexColumns returns the column each key part names, or "" for an
// expression part, which names none.
func indexColumns(idx *sqlparser.IndexDefinition) []string {
	cols := make([]string, len(idx.Columns))
	for i, c := range idx.Columns {
		cols[i] = c.Column.String()
	}
	return cols
}

// removeConstraintFromColumns removes the first constraint matching
// (cType, name) from table.Constraints and reverses its column flag
// effects. MySQL compares index and constraint names case-insensitively.
// Name may be empty for PrimaryKey (MySQL DROP PRIMARY KEY has
// no name). The slice deletion happens first so that RecomputeColumnUnique
// sees the post-drop state of the table.
func removeConstraintFromColumns(table *parser.Table, cType parser.ConstraintType, name string) {
	idx := slices.IndexFunc(table.Constraints, func(c parser.Constraint) bool {
		if c.Type != cType {
			return false
		}
		return name == "" || strings.EqualFold(c.Name, name)
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
