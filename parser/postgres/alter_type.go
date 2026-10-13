package postgres

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"

	"github.com/teandresmith/sqlgen/parser"
)

// parseAlterEnum applies ALTER TYPE … ADD VALUE [IF NOT EXISTS] [BEFORE |
// AFTER] and ALTER TYPE … RENAME VALUE, failing where PostgreSQL fails.
func (p *Parser) parseAlterEnum(stmt *pg_query.AlterEnumStmt) error {
	schema, name := p.splitQualifiedName(extractStringList(stmt.TypeName))
	e := p.findEnumByName(schema, name)
	if e == nil {
		return fmt.Errorf("alter type %s.%s: unknown enum", schema, name)
	}
	if stmt.OldVal != "" {
		return p.renameEnumValue(e, stmt.OldVal, stmt.NewVal)
	}
	if slices.Contains(e.Values, stmt.NewVal) {
		if stmt.SkipIfNewValExists {
			return nil
		}
		return fmt.Errorf("alter type %s.%s: enum label %q already exists", schema, name, stmt.NewVal)
	}
	at := len(e.Values)
	if stmt.NewValNeighbor != "" {
		at = slices.Index(e.Values, stmt.NewValNeighbor)
		if at < 0 {
			return fmt.Errorf("alter type %s.%s: %q is not an existing enum label", schema, name, stmt.NewValNeighbor)
		}
		if stmt.NewValIsAfter {
			at++
		}
	}
	e.Values = slices.Insert(e.Values, at, stmt.NewVal)
	return nil
}

// renameEnumValue applies ALTER TYPE … RENAME VALUE. PostgreSQL stores an
// enum constant by its label's OID, so every constant of the enum in a
// default, CHECK, generation expression or index predicate reads back with
// the new label.
func (p *Parser) renameEnumValue(e *parser.Enum, oldVal, newVal string) error {
	i := slices.Index(e.Values, oldVal)
	if i < 0 {
		return fmt.Errorf("alter type %s.%s: %q is not an existing enum label", e.Schema, e.Name, oldVal)
	}
	if slices.Contains(e.Values, newVal) {
		return fmt.Errorf("alter type %s.%s: enum label %q already exists", e.Schema, e.Name, newVal)
	}
	e.Values[i] = newVal
	for ti := range p.schema.Tables {
		t := &p.schema.Tables[ti]
		r := relabel{p: p, enum: e, schema: t.Schema, cols: map[string]bool{}, oldVal: oldVal, newVal: newVal}
		for _, c := range t.Columns {
			if p.isOfEnum(c.Type, t.Schema, e) {
				r.cols[c.Name] = true
			}
		}
		for ci := range t.Columns {
			c := &t.Columns[ci]
			c.Default = r.expr(c.Default, r.cols[c.Name])
			c.GeneratedExpr = r.expr(c.GeneratedExpr, false)
		}
		for ci := range t.Constraints {
			c := &t.Constraints[ci]
			c.CheckExpression = r.expr(c.CheckExpression, false)
			c.Where = r.expr(c.Where, false)
		}
	}
	for di := range p.schema.DomainTypes {
		dt := &p.schema.DomainTypes[di]
		r := relabel{p: p, enum: e, schema: dt.Schema, cols: map[string]bool{}, oldVal: oldVal, newVal: newVal}
		if p.isOfEnum(dt.BaseType, dt.Schema, e) {
			r.cols["value"] = true
		}
		for ci := range dt.Constraints {
			dt.Constraints[ci].CheckExpression = r.expr(dt.Constraints[ci].CheckExpression, false)
		}
	}
	return nil
}

// relabel renames an enum label in the expressions of one table or domain. A
// string constant is of the enum when it is cast to the enum, compared with or
// listed against one of cols (the columns of the enum's type), or the whole
// default of such a column.
type relabel struct {
	p              *Parser
	enum           *parser.Enum
	schema         string
	cols           map[string]bool
	oldVal, newVal string
}

// expr returns text with the label renamed; top says text is the default of
// a column of the enum's type.
func (r relabel) expr(text string, top bool) string {
	if text == "" {
		return text
	}
	node, err := parseExpr(text)
	if err != nil {
		return text
	}
	changed := false
	rename := func(n *pg_query.Node) {
		items := slices.Concat([]*pg_query.Node{n}, n.GetList().GetItems(), n.GetAArrayExpr().GetElements())
		for _, item := range items {
			if s := item.GetAConst().GetSval(); s != nil && s.Sval == r.oldVal {
				s.Sval, changed = r.newVal, true
			}
		}
	}
	if top {
		rename(node)
	}
	walkExpr(node, func(n *pg_query.Node) {
		if tc := n.GetTypeCast(); tc != nil && r.isEnumCast(tc) {
			rename(tc.Arg)
		}
		if ae := n.GetAExpr(); ae != nil {
			if r.isEnumOperand(ae.Lexpr) {
				rename(ae.Rexpr)
			}
			if r.isEnumOperand(ae.Rexpr) {
				rename(ae.Lexpr)
			}
		}
	})
	if !changed {
		return text
	}
	return deparseExpr(node)
}

// isEnumOperand reports whether n is one of r.cols or a cast to the enum.
func (r relabel) isEnumOperand(n *pg_query.Node) bool {
	if tc := n.GetTypeCast(); tc != nil {
		return r.isEnumCast(tc)
	}
	ref := n.GetColumnRef()
	if ref == nil {
		return false
	}
	fields := extractStringList(ref.Fields)
	return len(fields) > 0 && r.cols[fields[len(fields)-1]]
}

func (r relabel) isEnumCast(tc *pg_query.TypeCast) bool {
	return r.p.isOfEnum(r.p.typeName(tc.TypeName, r.schema), r.schema, r.enum)
}

// isOfEnum reports whether typ, written for schema, is the enum e or a domain
// over it.
func (p *Parser) isOfEnum(typ, schema string, e *parser.Enum) bool {
	for range len(p.schema.DomainTypes) + 1 {
		ref, isArray := parser.ResolveTypeRef(typ, schema)
		if isArray {
			return false
		}
		if ref.Schema == e.Schema && ref.Name == e.Name {
			return true
		}
		d := p.findDomainByName(ref.Schema, ref.Name)
		if d == nil {
			return false
		}
		typ, schema = d.BaseType, d.Schema
	}
	return false
}

// renameType applies ALTER TYPE … RENAME TO and ALTER DOMAIN … RENAME TO.
// PostgreSQL keys every use of a type on its OID, so each column, attribute
// and domain of the type takes the new name.
func (p *Parser) renameType(stmt *pg_query.RenameStmt) error {
	schema, name := p.splitQualifiedName(extractObjectNames(stmt.Object))
	qualified := schema + "." + name
	newName := stmt.Newname
	if _, ok := p.tableIndex[schema+"."+newName]; ok || p.isUserType(schema, newName) {
		return fmt.Errorf("alter type %s: type %s.%s already exists", qualified, schema, newName)
	}
	dt := p.findDomainByName(schema, name)
	e := p.findEnumByName(schema, name)
	ct := p.findCompositeTypeByName(schema, name)
	switch {
	case dt != nil:
		dt.Name = newName
	case stmt.RenameType == pg_query.ObjectType_OBJECT_DOMAIN:
		return fmt.Errorf("alter domain %s: unknown domain", qualified)
	case e != nil:
		e.Name = newName
	case ct != nil:
		if p.namespace(schema).relations[newName] {
			return fmt.Errorf("alter type %s: relation %s.%s already exists", qualified, schema, newName)
		}
		ct.Name = newName
	default:
		return fmt.Errorf("alter type %s: unknown type", qualified)
	}
	p.renameTypeReferences(schema, name, newName)
	return nil
}

// renameTypeReferences rewrites every type that names schema.oldName: column
// types of tables and views, composite attributes and domain base types.
func (p *Parser) renameTypeReferences(schema, oldName, newName string) {
	retype := func(typ, own string) string {
		ref, isArray := parser.ResolveTypeRef(typ, own)
		if ref.Schema != schema || ref.Name != oldName {
			return typ
		}
		out := newName
		if strings.Contains(typ, ".") {
			out = schema + "." + newName
		}
		if isArray {
			out += "[]"
		}
		return out
	}
	for i := range p.schema.Tables {
		t := &p.schema.Tables[i]
		for j := range t.Columns {
			t.Columns[j].Type = retype(t.Columns[j].Type, t.Schema)
		}
	}
	for i := range p.schema.Views {
		v := &p.schema.Views[i]
		for j := range v.Columns {
			v.Columns[j].Type = retype(v.Columns[j].Type, v.Schema)
		}
	}
	for i := range p.schema.CompositeTypes {
		ct := &p.schema.CompositeTypes[i]
		for j := range ct.Attributes {
			ct.Attributes[j].Type = retype(ct.Attributes[j].Type, ct.Schema)
		}
	}
	for i := range p.schema.DomainTypes {
		dt := &p.schema.DomainTypes[i]
		dt.BaseType = retype(dt.BaseType, dt.Schema)
	}
}

// renameAttribute applies ALTER TYPE … RENAME ATTRIBUTE.
func (p *Parser) renameAttribute(stmt *pg_query.RenameStmt) error {
	if stmt.Relation == nil {
		return nil
	}
	schema, name := cmp.Or(stmt.Relation.Schemaname, p.defaultSchema), stmt.Relation.Relname
	ct := p.findCompositeTypeByName(schema, name)
	if ct == nil {
		return fmt.Errorf("alter type %s.%s: unknown composite type", schema, name)
	}
	if slices.ContainsFunc(ct.Attributes, func(a parser.Attribute) bool { return a.Name == stmt.Newname }) {
		return fmt.Errorf("alter type %s.%s: attribute %s already exists", schema, name, stmt.Newname)
	}
	for i := range ct.Attributes {
		if ct.Attributes[i].Name == stmt.Subname {
			ct.Attributes[i].Name = stmt.Newname
			return nil
		}
	}
	return fmt.Errorf("alter type %s.%s: unknown attribute %s", schema, name, stmt.Subname)
}
