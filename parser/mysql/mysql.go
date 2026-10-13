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
	// columnKeys holds the keys each column definition of the statement being
	// applied declares, read from the statement text by tableElements or
	// readAlterColumnKeys.
	columnKeys map[*sqlparser.ColumnDefinition]columnKeys
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
		return p.parseAlterTable(s, sql)
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

	elements, columnKeys, err := p.tableElements(sql, stmt.TableSpec)
	if err != nil {
		return fmt.Errorf("create table %s: %w", tableName, err)
	}
	p.columnKeys = columnKeysOf(stmt.TableSpec.Columns, columnKeys)
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
// constraint, since vitess files that CHECK among the constraints. It also
// returns the keys each column declares, in column order. It reports an error
// when the entries it reads do not match spec.
func (p *Parser) tableElements(sql string, spec *sqlparser.TableSpec) ([]tableElement, []columnKeys, error) {
	var elements []tableElement
	var keys []columnKeys
	var tokens []int // the current entry's tokens outside nested parentheses
	addEntry := func() {
		entry := classifyTableElement(tokens)
		elements = append(elements, entry...)
		if len(entry) > 0 && entry[0] == columnElement {
			keys = append(keys, readColumnKeys(tokens))
		}
	}
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
				addEntry()
				break scan
			}
		case typ == ',' && depth == 1:
			addEntry()
			tokens = tokens[:0]
			continue
		}
		if depth == 1 {
			tokens = append(tokens, typ)
		}
	}
	if err := matchTableSpec(elements, spec); err != nil {
		return nil, nil, err
	}
	return elements, keys, nil
}

// columnKeys is the keys a column definition declares on its column. MySQL
// reads the attribute PRIMARY KEY, or KEY alone, as the primary key and
// UNIQUE [KEY] as a unique key, and makes one key of each kind however many
// times the attributes repeat (measured on MySQL 8.0: `a int UNIQUE NOT NULL
// UNIQUE PRIMARY KEY UNIQUE KEY` makes PRIMARY and one unique key a). vitess
// keeps only the last attribute (ColumnTypeOptions.KeyOpt), so the keys are
// read from the statement's tokens, which hold every attribute vitess parses.
type columnKeys struct {
	primary, unique bool
}

// readColumnKeys returns the keys the column definition with the top-level
// tokens tokens, the column name first, declares. The key keywords are
// reserved in MySQL, so no name, type or other attribute of the column is one
// unquoted.
func readColumnKeys(tokens []int) columnKeys {
	var k columnKeys
	for i := 1; i < len(tokens); i++ {
		switch tokens[i] {
		case sqlparser.PRIMARY:
			k.primary = true
			i++ // KEY
		case sqlparser.KEY:
			k.primary = true
		case sqlparser.UNIQUE:
			k.unique = true
			if i+1 < len(tokens) && tokens[i+1] == sqlparser.KEY {
				i++
			}
		}
	}
	return k
}

// columnKeysOf maps each column definition in defs to the keys in keys at
// the same position.
func columnKeysOf(defs []*sqlparser.ColumnDefinition, keys []columnKeys) map[*sqlparser.ColumnDefinition]columnKeys {
	m := make(map[*sqlparser.ColumnDefinition]columnKeys, len(defs))
	for i, def := range defs {
		m[def] = keys[i]
	}
	return m
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

	// Column-level keys, the primary key first as MySQL adds them.
	var constraints []parser.Constraint
	keys := p.columnKeys[col]
	if keys.primary {
		c.Nullable = false
		constraints = append(constraints, parser.Constraint{Name: "PRIMARY", Type: parser.PrimaryKey, Columns: []string{c.Name}})
	}
	if keys.unique {
		constraints = append(constraints, parser.Constraint{Type: parser.Unique, Columns: []string{c.Name}})
	}

	// Inline FK reference.
	if opts.Reference != nil {
		ref := opts.Reference
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
// in the table's ExpressionIndexes, not listed, but it keeps its name, an
// unnamed one is named functional_index, and one whose leading columns match
// a foreign key's serves as its index.
func (p *Parser) addKey(table *parser.Table, key parser.Constraint, generated bool) {
	key.Columns = declaredColumns(table, key.Columns)
	built := p.generated[table.Name]
	if built == nil {
		built = make(map[string]bool)
		p.generated[table.Name] = built
	}
	if generated && slices.ContainsFunc(table.ExpressionIndexes, func(k parser.ExpressionIndex) bool {
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
		for _, k := range table.ExpressionIndexes {
			names = append(names, k.Name)
		}
		named := key
		if expr {
			named.Columns = []string{"functional_index"}
		}
		key.Name = parser.DeriveMySQLConstraintName(table.Name, named, names)
	}
	if expr {
		table.ExpressionIndexes = append(table.ExpressionIndexes, parser.ExpressionIndex{Name: key.Name, Columns: key.Columns})
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
// brings the index MySQL builds for it, unless a key in later, the keys the
// statement's later clauses add, makes it redundant (replacedLater); indexName
// is the index name its FOREIGN KEY clause declares.
func (p *Parser) addConstraints(table *parser.Table, constraints []parser.Constraint, indexName string, later []tableKey) {
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
		if c.Type != parser.ForeignKey {
			continue
		}
		if key := (tableKey{Constraint: foreignKeyIndex(c, indexName), built: true}); !replacedLater(key, later) {
			p.addKey(table, key.Constraint, true)
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

func (p *Parser) parseAlterTable(stmt *sqlparser.AlterTable, sql string) error {
	tableName := stmt.Table.Name.String()
	table, ok := p.lookupTable(tableName)
	if !ok {
		return fmt.Errorf("table %s not found", tableName)
	}
	if err := p.readAlterColumnKeys(stmt, sql); err != nil {
		return fmt.Errorf("alter table %s: %w", tableName, err)
	}

	// MySQL applies the statement's drops to the keys the table has, then its
	// key renames, wherever they sit among the clauses, and only then adds the
	// new keys in clause order, each named against the keys before it
	// (measured on MySQL 8.0). Applied in clause order, `ADD PRIMARY KEY
	// (code), DROP PRIMARY KEY` would lose the key it just added, and `ADD KEY
	// (a), DROP INDEX a` would name the new key a_2.
	opts := stmt.AlterOptions
	for _, opt := range opts {
		if o, ok := opt.(*sqlparser.DropKey); ok {
			p.dropKey(table, o)
		}
	}
	for _, opt := range opts {
		if o, ok := opt.(*sqlparser.RenameIndex); ok {
			p.renameKey(table, o)
		}
	}
	// As in CREATE TABLE, MySQL builds no index for a foreign key when a key a
	// later clause adds makes it redundant (replacedLater). after[i] is where
	// the keys the clauses after clause i add start in added.
	var added []tableKey
	after := make([]int, len(opts))
	for i, opt := range opts {
		if key, ok := p.addedKey(opt); ok {
			added = append(added, key)
		}
		after[i] = len(added)
	}
	for i, opt := range opts {
		switch opt.(type) {
		case *sqlparser.DropKey, *sqlparser.RenameIndex:
			continue
		}
		if err := p.applyAlterOption(table, opt, added[after[i]:]); err != nil {
			return err
		}
	}

	return nil
}

// addedKey returns the key an ADD INDEX or ADD FOREIGN KEY clause adds, for a
// foreign key the index MySQL builds for it, and true, or false for any other
// clause. Column-inline keys are left out: one covers a single column, so a
// foreign key index it makes redundant is replaced as soon as it is added.
func (p *Parser) addedKey(opt sqlparser.AlterOption) (tableKey, bool) {
	switch o := opt.(type) {
	case *sqlparser.AddIndexDefinition:
		return tableKey{Constraint: p.parseIndex(o.IndexDefinition)}, true
	case *sqlparser.AddConstraintDefinition:
		if fk, ok := o.ConstraintDefinition.Details.(*sqlparser.ForeignKeyDefinition); ok {
			key := foreignKeyIndex(p.parseConstraintDef(o.ConstraintDefinition), fk.IndexName.String())
			return tableKey{Constraint: key, built: true}, true
		}
	}
	return tableKey{}, false
}

// dropKeyType returns the kind of key o drops. vitess parses the ALTER TABLE
// clause DROP INDEX or DROP KEY `PRIMARY` as a normal-key drop, and DROP
// CONSTRAINT `PRIMARY` as a check drop, but a MySQL primary key is always
// named PRIMARY, and index and constraint names compare case-insensitively,
// so MySQL drops the primary key.
func dropKeyType(o *sqlparser.DropKey) sqlparser.DropKeyType {
	if (o.Type == sqlparser.NormalKeyType || o.Type == sqlparser.CheckKeyType) && strings.EqualFold(o.Name.String(), "PRIMARY") {
		return sqlparser.PrimaryKeyType
	}
	return o.Type
}

func (p *Parser) applyAlterOption(table *parser.Table, opt sqlparser.AlterOption, later []tableKey) error {
	switch o := opt.(type) {
	case *sqlparser.AddColumns:
		for _, col := range o.Columns {
			c, constraints := p.parseColumn(table.Name, col)
			table.Columns = append(table.Columns, c)
			p.addConstraints(table, constraints, "", nil)
		}
	case *sqlparser.DropColumn:
		name := declaredColumn(table, o.Name.Name.String())
		alterDropColumn(table, name)
		dropExprKeyColumn(table, name)
	case *sqlparser.ModifyColumn:
		return p.alterModifyColumn(table, o)
	case *sqlparser.AddIndexDefinition:
		p.addConstraints(table, []parser.Constraint{p.parseIndex(o.IndexDefinition)}, "", later)
	case *sqlparser.AddConstraintDefinition:
		var indexName string
		if fk, ok := o.ConstraintDefinition.Details.(*sqlparser.ForeignKeyDefinition); ok {
			indexName = fk.IndexName.String()
		}
		p.addConstraints(table, []parser.Constraint{p.parseConstraintDef(o.ConstraintDefinition)}, indexName, later)
	case *sqlparser.RenameColumn:
		oldName, newName := declaredColumn(table, o.OldName.Name.String()), o.NewName.Name.String()
		if err := parser.RenameColumn(table, "mysql", oldName, newName); err != nil {
			return fmt.Errorf("rename column %s: %w", table.Name, err)
		}
		parser.RenameColumnReferences(p.schema.Tables, "mysql", table.Schema, table.Name, oldName, newName)
		renameExprKeyColumn(table, oldName, newName)
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
	p.addConstraints(table, constraints, "", nil)
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

// readAlterColumnKeys records the keys each column definition of the ALTER
// TABLE statement stmt declares, read from its text sql: the ADD, MODIFY and
// CHANGE COLUMN clauses, in clause order. It reports an error when the
// definitions it reads do not match stmt's.
func (p *Parser) readAlterColumnKeys(stmt *sqlparser.AlterTable, sql string) error {
	var defs []*sqlparser.ColumnDefinition
	for _, opt := range stmt.AlterOptions {
		switch o := opt.(type) {
		case *sqlparser.AddColumns:
			defs = append(defs, o.Columns...)
		case *sqlparser.ModifyColumn:
			defs = append(defs, o.NewColDefinition)
		case *sqlparser.ChangeColumn:
			defs = append(defs, o.NewColDefinition)
		}
	}
	keys := p.alterColumnKeys(sql)
	if len(keys) != len(defs) {
		return fmt.Errorf("cannot recover column keys: read %d column definitions, parsed %d", len(keys), len(defs))
	}
	p.columnKeys = columnKeysOf(defs, keys)
	return nil
}

// alterToken is a token of an ALTER TABLE statement and how deep in
// parentheses it sits; a parenthesis sits at the depth outside it.
type alterToken struct {
	typ, depth int
}

// alterColumnKeys returns the keys each column definition of the ALTER TABLE
// statement sql declares, in clause order: the clauses ADD [COLUMN] col_def,
// ADD [COLUMN] (col_def, ...), MODIFY [COLUMN] col_def and CHANGE [COLUMN]
// old_name col_def. The clauses end at a partition clause, which declares no
// column.
func (p *Parser) alterColumnKeys(sql string) []columnKeys {
	var tokens []alterToken
	depth := 0
	tkn := p.vparser.NewStringTokenizer(sql)
	for {
		typ, _ := tkn.Scan()
		if typ == 0 || typ == sqlparser.LEX_ERROR {
			break
		}
		switch typ {
		case sqlparser.COMMENT:
			continue
		case '(':
			tokens = append(tokens, alterToken{typ, depth})
			depth++
			continue
		case ')':
			depth--
		}
		tokens = append(tokens, alterToken{typ, depth})
	}

	// The clauses follow ALTER TABLE and the table name, which may be
	// qualified.
	start := slices.IndexFunc(tokens, func(t alterToken) bool { return t.typ == sqlparser.TABLE }) + 2
	if start < len(tokens) && tokens[start].typ == '.' {
		start += 2
	}
	var keys []columnKeys
	var clause []alterToken
	for _, t := range tokens[min(start, len(tokens)):] {
		if t.depth == 0 && t.typ == sqlparser.PARTITION {
			break
		}
		if t.depth == 0 && t.typ == ',' {
			keys = append(keys, clauseColumnKeys(clause)...)
			clause = clause[:0]
			continue
		}
		clause = append(clause, t)
	}
	return append(keys, clauseColumnKeys(clause)...)
}

// clauseColumnKeys returns the keys each column definition of the ALTER TABLE
// clause clause declares, or none when it defines no column.
func clauseColumnKeys(clause []alterToken) []columnKeys {
	if len(clause) == 0 {
		return nil
	}
	verb, rest := clause[0].typ, clause[1:]
	if len(rest) > 0 && rest[0].typ == sqlparser.COLUMN {
		rest = rest[1:]
	}
	switch verb {
	case sqlparser.ADD:
		if len(rest) > 0 && rest[0].typ == '(' {
			return listColumnKeys(rest)
		}
		def := tokensAt(rest, 0)
		if entry := classifyTableElement(def); len(entry) == 0 || entry[0] != columnElement {
			return nil
		}
		return []columnKeys{readColumnKeys(def)}
	case sqlparser.MODIFY:
		return []columnKeys{readColumnKeys(tokensAt(rest, 0))}
	case sqlparser.CHANGE:
		def := tokensAt(rest, 0)
		return []columnKeys{readColumnKeys(def[min(1, len(def)):])}
	}
	return nil
}

// listColumnKeys returns the keys each column definition of the
// parenthesized list tokens, ADD's (col_def, ...), declares.
func listColumnKeys(tokens []alterToken) []columnKeys {
	var keys []columnKeys
	var def []int
	for _, t := range tokens[1:] {
		switch {
		case t.depth == 0:
			return append(keys, readColumnKeys(def))
		case t.depth == 1 && t.typ == ',':
			keys = append(keys, readColumnKeys(def))
			def = def[:0]
		case t.depth == 1:
			def = append(def, t.typ)
		}
	}
	return keys
}

// tokensAt returns the types of the tokens in tokens at depth.
func tokensAt(tokens []alterToken, depth int) []int {
	var types []int
	for _, t := range tokens {
		if t.depth == depth {
			types = append(types, t.typ)
		}
	}
	return types
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
	renameExprKeyColumn(table, oldName, newCol.Name)
	p.addConstraints(table, constraints, "", nil)
	return true
}

// redefineColumn returns the column that MODIFY or CHANGE COLUMN leaves in
// place of old. The new definition replaces the type, nullability, default,
// AUTO_INCREMENT and comment, but the table's keys outlive it: MySQL keeps the
// PRIMARY KEY and every UNIQUE index on the column, declared inline or at table
// level, and every foreign key on it (measured on MySQL 8.0, SHOW CREATE TABLE
// after the ALTER).
// The definition alone cannot see them, so the key flags they set on old carry
// over, and a primary-key column stays NOT NULL as MySQL makes it. Without
// this the table loses its primary key and drops out of generation. The
// foreign keys stay in the table's constraints, which give the column its
// FKReference when the table is canonicalized.
func redefineColumn(old, newCol parser.Column) parser.Column {
	if old.PrimaryKey {
		newCol.PrimaryKey = true
		newCol.Nullable = false
	}
	newCol.Unique = newCol.Unique || old.Unique
	return newCol
}

// dropKey applies a DROP clause of ALTER TABLE. Only a clause that drops a
// UNIQUE or INDEX by name drops an expression key of that name or clears the
// mark of an index MySQL built for a foreign key: DROP FOREIGN KEY keeps that
// index, and a later key still makes it redundant (measured on MySQL 8.0).
func (p *Parser) dropKey(table *parser.Table, o *sqlparser.DropKey) {
	name := o.Name.String()
	var kinds []parser.ConstraintType // the kinds of key the clause drops by name
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
		kinds = []parser.ConstraintType{parser.Unique, parser.Index}
	case sqlparser.CheckKeyType:
		// vitess parses DROP CHECK and DROP CONSTRAINT alike. DROP CONSTRAINT
		// drops the CHECK, FOREIGN KEY or UNIQUE of that name (MySQL 8.0.19+),
		// and MySQL rejects a name two of them share.
		if !removeConstraintFromColumns(table, parser.Check, name) &&
			!removeConstraintFromColumns(table, parser.ForeignKey, name) {
			kinds = []parser.ConstraintType{parser.Unique}
		}
	}
	if len(kinds) == 0 {
		return
	}
	for _, kind := range kinds {
		removeConstraintFromColumns(table, kind, name)
	}
	maps.DeleteFunc(p.generated[table.Name], func(built string, _ bool) bool {
		return strings.EqualFold(built, name)
	})
	if i := slices.IndexFunc(table.ExpressionIndexes, func(k parser.ExpressionIndex) bool {
		return strings.EqualFold(k.Name, name)
	}); i >= 0 {
		table.ExpressionIndexes = slices.Concat(table.ExpressionIndexes[:i], table.ExpressionIndexes[i+1:])
	}
}

// renameKey applies RENAME INDEX or RENAME KEY, which MySQL matches
// case-insensitively. A renamed index MySQL built for a foreign key is no
// longer one: a later key does not make it redundant, even after it is renamed
// back (measured on MySQL 8.0).
func (p *Parser) renameKey(table *parser.Table, o *sqlparser.RenameIndex) {
	oldName, newName := o.OldName.String(), o.NewName.String()
	for i := range table.Constraints {
		if c := &table.Constraints[i]; (c.Type == parser.Unique || c.Type == parser.Index) && strings.EqualFold(c.Name, oldName) {
			c.Name = newName
			break
		}
	}
	maps.DeleteFunc(p.generated[table.Name], func(built string, _ bool) bool {
		return strings.EqualFold(built, oldName)
	})
	for i := range table.ExpressionIndexes {
		if k := &table.ExpressionIndexes[i]; strings.EqualFold(k.Name, oldName) {
			k.Name = newName
		}
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
	built, inline := p.generated[oldName], p.inline[oldName]
	delete(p.generated, oldName)
	delete(p.inline, oldName)
	p.generated[newName] = built
	p.inline[newName] = inline
	delete(p.tableIndex, oldName)
	p.tableIndex[newName] = idx
	return nil
}

// renameExprKeyColumn renames a column in the table's expression keys, as
// MySQL renames it in every key.
func renameExprKeyColumn(table *parser.Table, oldName, newName string) {
	for _, k := range table.ExpressionIndexes {
		for i, name := range k.Columns {
			if name == oldName {
				k.Columns[i] = newName
			}
		}
	}
}

// dropExprKeyColumn takes a dropped column out of the table's expression
// keys, as MySQL takes it out of every key.
func dropExprKeyColumn(table *parser.Table, colName string) {
	for i := range table.ExpressionIndexes {
		k := &table.ExpressionIndexes[i]
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
// effects, and reports whether it found one. MySQL compares index and
// constraint names case-insensitively. Name may be empty for PrimaryKey
// (MySQL DROP PRIMARY KEY has no name). The slice deletion happens first so
// that RecomputeColumnUnique sees the post-drop state of the table.
func removeConstraintFromColumns(table *parser.Table, cType parser.ConstraintType, name string) bool {
	idx := slices.IndexFunc(table.Constraints, func(c parser.Constraint) bool {
		if c.Type != cType {
			return false
		}
		return name == "" || strings.EqualFold(c.Name, name)
	})
	if idx < 0 {
		return false
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
		parser.CanonicalizeConstraints(table, "mysql")
	case parser.Check, parser.Index:
		// No column flags to reverse.
	}
	return true
}
