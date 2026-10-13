package postgres

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"

	"github.com/teandresmith/sqlgen/parser"
)

// pgSequence is a sequence the DDL creates: with CREATE SEQUENCE, or for a
// serial or identity column. The model holds no sequences, but a sequence
// takes a relation name, and a column that owns one reads as the introspector
// reads it: a nextval default of an owning column is AutoIncrement with no
// default.
type pgSequence struct {
	schema, name string
	table        string // the owning column's qualified table; "" when unowned
	column       string
	// identity marks an identity column's sequence. The column depends on it
	// as an identity, not through OWNED BY, so it is no serial default.
	identity bool
}

// sequence returns the sequence schema.name, or nil.
func (p *Parser) sequence(schema, name string) *pgSequence {
	for _, s := range p.sequences {
		if s.schema == schema && s.name == name {
			return s
		}
	}
	return nil
}

// ownedSequence returns a sequence column of table owns through OWNED BY (or
// as a serial column), or through its identity when identity is set, or nil.
func (p *Parser) ownedSequence(table, column string, identity bool) *pgSequence {
	for _, s := range p.sequences {
		if s.table == table && s.column == column && s.identity == identity {
			return s
		}
	}
	return nil
}

// addColumnSequence records the sequence PostgreSQL creates for a serial or
// identity column cd of table. PostgreSQL names it before the table's indexes.
func (p *Parser) addColumnSequence(table *parser.Table, cd *pg_query.ColumnDef) {
	if isSerial(p.typeName(cd.TypeName, table.Schema)) {
		p.addImplicitSequence(table, cd.Colname, false, nil)
	}
	for _, c := range cd.Constraints {
		if cn := c.GetConstraint(); cn != nil && cn.Contype == pg_query.ConstrType_CONSTR_IDENTITY {
			p.addImplicitSequence(table, cd.Colname, true, cn.Options)
		}
	}
}

// addImplicitSequence records the sequence of a serial or identity column:
// the one its SEQUENCE NAME option names, or <table>_<column>_seq.
func (p *Parser) addImplicitSequence(table *parser.Table, column string, identity bool, options []*pg_query.Node) {
	seq := &pgSequence{
		schema:   table.Schema,
		table:    table.Schema + "." + table.Name,
		column:   column,
		identity: identity,
	}
	if names := sequenceNameOption(options); len(names) > 0 {
		seq.schema, seq.name = p.splitQualifiedName(names)
	} else {
		seq.name = parser.DerivePostgresSequenceName(table.Name, column, p.namespace(table.Schema).taken(parser.Index))
	}
	p.sequences = append(p.sequences, seq)
}

// sequenceNameOption returns the name parts of an identity's SEQUENCE NAME.
func sequenceNameOption(options []*pg_query.Node) []string {
	for _, o := range options {
		if d := o.GetDefElem(); d != nil && d.Defname == "sequence_name" {
			return extractObjectNames(d.Arg)
		}
	}
	return nil
}

func isSerial(typ string) bool {
	switch strings.ToLower(typ) {
	case "serial", "serial4", "bigserial", "serial8", "smallserial", "serial2":
		return true
	}
	return false
}

func isNextval(expr string) bool {
	return strings.Contains(strings.ToLower(expr), "nextval(")
}

func (p *Parser) parseCreateSequence(stmt *pg_query.CreateSeqStmt) error {
	schema := cmp.Or(stmt.Sequence.Schemaname, p.defaultSchema)
	if p.sequence(schema, stmt.Sequence.Relname) != nil {
		if stmt.IfNotExists {
			return nil
		}
		return fmt.Errorf("sequence %s.%s already exists", schema, stmt.Sequence.Relname)
	}
	seq := &pgSequence{schema: schema, name: stmt.Sequence.Relname}
	p.sequences = append(p.sequences, seq)
	p.applySequenceOptions(seq, stmt.Options)
	return nil
}

// parseAlterSequence applies ALTER SEQUENCE … OWNED BY. A sequence the DDL
// did not create (one from the database under input.source: both) is
// recorded when an ALTER SEQUENCE names it.
func (p *Parser) parseAlterSequence(stmt *pg_query.AlterSeqStmt) error {
	schema := cmp.Or(stmt.Sequence.Schemaname, p.defaultSchema)
	seq := p.sequence(schema, stmt.Sequence.Relname)
	if seq == nil {
		if stmt.MissingOk {
			return nil
		}
		seq = &pgSequence{schema: schema, name: stmt.Sequence.Relname}
		p.sequences = append(p.sequences, seq)
	}
	p.applySequenceOptions(seq, stmt.Options)
	return nil
}

// applySequenceOptions applies the OWNED BY option. Ownership moves a
// nextval default between the forms the introspector reads: the new owner's
// default becomes AutoIncrement, and the old owner's default comes back.
func (p *Parser) applySequenceOptions(seq *pgSequence, options []*pg_query.Node) {
	for _, o := range options {
		d := o.GetDefElem()
		if d == nil || d.Defname != "owned_by" {
			continue
		}
		oldTable, oldColumn := seq.table, seq.column
		names := extractObjectNames(d.Arg)
		switch {
		case len(names) == 1 && names[0] == "none":
			seq.table, seq.column = "", ""
		case len(names) >= 2:
			table := p.findTableByNames(names[:len(names)-1])
			if table == nil {
				continue
			}
			seq.table, seq.column = table.Schema+"."+table.Name, names[len(names)-1]
			if col := parser.ColumnByName(table, seq.column); col != nil {
				p.hideOwnedDefault(table, col)
			}
		default:
			continue
		}
		if oldTable != "" && (oldTable != seq.table || oldColumn != seq.column) {
			p.restoreDefault(oldTable, oldColumn, seq)
		}
	}
}

// hideOwnedDefault reads col as the introspector reads it: a nextval default
// of a column that owns a sequence is AutoIncrement with no default.
func (p *Parser) hideOwnedDefault(table *parser.Table, col *parser.Column) {
	if isNextval(col.Default) && p.ownedSequence(table.Schema+"."+table.Name, col.Name, false) != nil {
		col.Default = ""
		col.AutoIncrement = true
	}
}

// restoreDefault gives back the nextval default of a column that no longer
// owns seq, if hideOwnedDefault hid it and the column owns no other sequence.
// The default is written as the catalog prints it.
func (p *Parser) restoreDefault(tableKey, column string, seq *pgSequence) {
	idx, ok := p.tableIndex[tableKey]
	if !ok {
		return
	}
	col := parser.ColumnByName(&p.schema.Tables[idx], column)
	if col == nil || !col.AutoIncrement || col.Default != "" ||
		p.ownedSequence(tableKey, column, false) != nil || p.ownedSequence(tableKey, column, true) != nil {
		return
	}
	ref := quoteIdent(seq.name)
	if seq.schema != p.defaultSchema {
		ref = quoteIdent(seq.schema) + "." + ref
	}
	col.Default = "nextval('" + strings.ReplaceAll(ref, "'", "''") + "')"
}

var plainIdent = regexp.MustCompile(`^[a-z_][a-z0-9_$]*$`)

// quoteIdent quotes name as PostgreSQL's quote_identifier does for a name
// that is not a plain lower-case identifier.
func quoteIdent(name string) string {
	if plainIdent.MatchString(name) {
		return name
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// alterColumnDefault applies SET DEFAULT and DROP DEFAULT. PostgreSQL rejects
// both on an identity column, so AutoIncrement follows the default alone.
func (p *Parser) alterColumnDefault(table *parser.Table, cmd *pg_query.AlterTableCmd) {
	col := parser.ColumnByName(table, cmd.Name)
	if col == nil {
		return
	}
	col.Default = deparseExpr(cmd.Def) // "" for DROP DEFAULT
	col.AutoIncrement = isNextval(col.Default)
	p.hideOwnedDefault(table, col)
}

// alterAddIdentity applies ALTER COLUMN … ADD GENERATED … AS IDENTITY, which
// pg_dump writes for every identity column.
func (p *Parser) alterAddIdentity(table *parser.Table, cmd *pg_query.AlterTableCmd) {
	col := parser.ColumnByName(table, cmd.Name)
	if col == nil {
		return
	}
	col.AutoIncrement = true
	col.Nullable = false
	p.addImplicitSequence(table, col.Name, true, cmd.Def.GetConstraint().GetOptions())
}

// alterDropIdentity applies ALTER COLUMN … DROP IDENTITY [IF EXISTS], which
// drops the identity's sequence. With IF EXISTS on a column the DDL did not
// make an identity, the column is left alone.
func (p *Parser) alterDropIdentity(table *parser.Table, cmd *pg_query.AlterTableCmd) {
	col := parser.ColumnByName(table, cmd.Name)
	if col == nil {
		return
	}
	seq := p.ownedSequence(table.Schema+"."+table.Name, col.Name, true)
	if seq == nil && cmd.MissingOk {
		return
	}
	p.sequences = slices.DeleteFunc(p.sequences, func(s *pgSequence) bool { return s == seq })
	col.AutoIncrement = false
}

// dropSequence applies DROP SEQUENCE. An owner's hidden nextval default goes
// with the sequence under CASCADE, and otherwise comes back.
func (p *Parser) dropSequence(schema, name string, cascade bool) {
	seq := p.sequence(schema, name)
	if seq == nil || seq.identity {
		return // PostgreSQL drops an identity's sequence only with the identity.
	}
	p.sequences = slices.DeleteFunc(p.sequences, func(s *pgSequence) bool { return s == seq })
	if seq.table == "" {
		return
	}
	if !cascade {
		p.restoreDefault(seq.table, seq.column, seq)
		return
	}
	if idx, ok := p.tableIndex[seq.table]; ok {
		col := parser.ColumnByName(&p.schema.Tables[idx], seq.column)
		if col != nil && col.Default == "" && p.ownedSequence(seq.table, seq.column, true) == nil {
			col.AutoIncrement = false
		}
	}
}

// renameSequence applies ALTER SEQUENCE … RENAME TO.
func (p *Parser) renameSequence(rv *pg_query.RangeVar, newName string) {
	if rv == nil {
		return
	}
	if seq := p.sequence(cmp.Or(rv.Schemaname, p.defaultSchema), rv.Relname); seq != nil {
		seq.name = newName
	}
}

// moveSequences carries the sequences a table's columns own along with a
// renamed or moved table: PostgreSQL moves them into the table's new schema.
func (p *Parser) moveSequences(oldKey, newKey, newSchema string) {
	for _, s := range p.sequences {
		if s.table == oldKey {
			s.table = newKey
			s.schema = newSchema
		}
	}
}

// renameSequenceColumn follows a column rename.
func (p *Parser) renameSequenceColumn(tableKey, oldName, newName string) {
	for _, s := range p.sequences {
		if s.table == tableKey && s.column == oldName {
			s.column = newName
		}
	}
}

// dropOwnedSequences drops the sequences the table's column owns, or every
// column's when column is "", as PostgreSQL drops them with their owner.
func (p *Parser) dropOwnedSequences(tableKey, column string) {
	p.sequences = slices.DeleteFunc(p.sequences, func(s *pgSequence) bool {
		return s.table == tableKey && (column == "" || s.column == column)
	})
}
