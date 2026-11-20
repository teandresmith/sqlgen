package gen

import (
	"slices"

	"github.com/teandresmith/sqlgen/parser"
)

// inForeignKey reports whether column is a member of any FOREIGN KEY
// constraint on table (PRD §8.2, §8.6).
func inForeignKey(table *parser.Table, column string) bool {
	return slices.ContainsFunc(table.Constraints, func(c parser.Constraint) bool {
		return c.Type == parser.ForeignKey && slices.Contains(c.Columns, column)
	})
}

// foreignKeyInto reports whether column is a member of a FOREIGN KEY on table
// that references the table refName (in refSchema, when one is given).
func foreignKeyInto(table *parser.Table, column, refSchema, refName string) bool {
	return slices.ContainsFunc(table.Constraints, func(c parser.Constraint) bool {
		return c.Type == parser.ForeignKey && slices.Contains(c.Columns, column) && referencesTable(c, refSchema, refName)
	})
}

func referencesTable(c parser.Constraint, schema, name string) bool {
	return c.ReferenceTable == name && (schema == "" || c.ReferenceSchema == "" || c.ReferenceSchema == schema)
}

// stampKeyConstraintFacts fills r.SharedMembers and r.Match for an O2O or O2M
// edge from the FOREIGN KEYs of the table holding its key. The edge's own
// constraint is the one over exactly the key's columns into the referenced
// table; a declared edge has one only when the schema declares that key too.
// Every key member another FOREIGN KEY also holds is shared, which pins it
// (PRD §9.9.2).
func stampKeyConstraintFacts(schema *parser.Schema, r *RelationshipContext, sourceSchema, sourceName string) {
	if r.Type != parser.OneToMany && r.Type != parser.OneToOne {
		return
	}
	holderSchema, holderName := sourceSchema, sourceName
	refSchema, refName := r.TargetSchema, r.TargetTable
	if r.Type == parser.OneToMany || r.FKOnTarget {
		holderSchema, holderName = r.TargetSchema, r.TargetTable
		refSchema, refName = sourceSchema, sourceName
	}
	holder := findSchemaTable(schema, holderSchema, holderName)
	if holder == nil {
		return
	}
	locals := keyLocals(*r)
	own := func(c parser.Constraint) bool {
		return sameColumnSet(c.Columns, locals) && referencesTable(c, refSchema, refName)
	}
	for _, c := range holder.Constraints {
		if c.Type == parser.ForeignKey && own(c) {
			r.Match = c.Match
			break
		}
	}
	for _, m := range locals {
		if slices.ContainsFunc(holder.Constraints, func(c parser.Constraint) bool {
			return c.Type == parser.ForeignKey && slices.Contains(c.Columns, m) && !own(c)
		}) {
			r.SharedMembers = append(r.SharedMembers, m)
		}
	}
}

// keyLocals returns an O2O / O2M edge's FK columns in key order. An edge built
// without key pairs (a unit fixture) has its one FKColumn.
func keyLocals(r RelationshipContext) []string {
	if len(r.Key) == 0 {
		if r.FKColumn == "" {
			return nil
		}
		return []string{r.FKColumn}
	}
	cols := make([]string, len(r.Key))
	for i, p := range r.Key {
		cols[i] = p.Local
	}
	return cols
}

// parentKeyColumn returns the column on the parent that a to-many or has-one
// edge's key references, and the M2M junction's local key references: the
// first referenced column, or the parent's primary key for an edge built
// without key pairs.
func parentKeyColumn(r RelationshipContext, parent *TableContext) (ColumnContext, bool) {
	name := ""
	switch {
	case r.Type == parser.ManyToMany && len(r.JunctionLocal) > 0:
		name = r.JunctionLocal[0].Referenced
	case r.Type != parser.ManyToMany && len(r.Key) > 0:
		name = r.Key[0].Referenced
	}
	if name != "" {
		if col, ok := columnBySQLName(parent.Columns, name); ok {
			return col, true
		}
	}
	if len(parent.PKColumns) == 0 {
		return ColumnContext{}, false
	}
	return parent.PKColumns[0], true
}

// keyMember is one FK column of a to-many or has-one edge, read off the target
// that holds it.
type keyMember struct {
	column   string
	nullable bool
	pinned   bool
	tenant   bool
}

// edgeKeyMembers classifies the key of an O2M or has-one edge on its target
// (PRD §9.9.2). A member is pinned when it is the target's tenant column, a
// member of its primary key, or a member of another of its FOREIGN KEYs.
func edgeKeyMembers(rel RelationshipContext, target *TableContext) []keyMember {
	locals := keyLocals(rel)
	members := make([]keyMember, 0, len(locals))
	for _, name := range locals {
		col, _ := columnBySQLName(target.Columns, name)
		members = append(members, keyMember{
			column:   name,
			nullable: col.Nullable,
			pinned:   col.Tenant || col.PrimaryKey || slices.Contains(rel.SharedMembers, name),
			tenant:   col.Tenant,
		})
	}
	return members
}

// keyParents returns the parent column each key pair references, or the
// parent's key for an edge built without pairs.
func keyParents(tc *TableContext, pairs []parser.KeyPair, fallback ColumnContext) []ColumnContext {
	if len(pairs) == 0 {
		return []ColumnContext{fallback}
	}
	parents := make([]ColumnContext, len(pairs))
	for i, p := range pairs {
		parents[i] = fallback
		if col, ok := columnBySQLName(tc.Columns, p.Referenced); ok {
			parents[i] = col
		}
	}
	return parents
}

// keyRewrite is which rewrite verbs an edge's key admits, before the verbs
// mask and the rendering checks (PRD §9.9.4, E8).
type keyRewrite struct {
	connect bool
	// unlink covers `disconnect` and `clear`, which null the same members.
	unlink bool
}

// keyRewriteVerbs applies E8's key rules. Every verb needs a free member and
// every free member nullable. Under PostgreSQL MATCH FULL a key may not mix
// NULL and non-NULL members, so unlinking needs every member free, and a key
// with a pinned member is never unparented, so `connect` there only reparents.
// The parser records MATCH FULL on PostgreSQL only.
func keyRewriteVerbs(members []keyMember, match parser.MatchType, allowReparent bool) keyRewrite {
	free, pinned := 0, 0
	for _, m := range members {
		if m.pinned {
			pinned++
			continue
		}
		if !m.nullable {
			return keyRewrite{}
		}
		free++
	}
	if free == 0 {
		return keyRewrite{}
	}
	if match == parser.MatchFull && pinned > 0 {
		return keyRewrite{connect: allowReparent}
	}
	return keyRewrite{connect: true, unlink: true}
}

// declaredKeyConstraints names, for each key of a declared edge, the FOREIGN
// KEY whose column pairs equal that key, in any order, and which references the
// key's table: one name for O2O / O2M, the junction's two for M2M. Of
// identical keys it names the one detection keeps, the first by name, then by
// declaration. A key no FOREIGN KEY declares gets "" (PRD §13.4, §30.4.2).
func declaredKeyConstraints(schema *parser.Schema, r RelationshipContext, sourceSchema, sourceName string) []string {
	find := func(holderSchema, holderName, refSchema, refName string, key []parser.KeyPair) string {
		return foreignKeyOver(findSchemaTable(schema, holderSchema, holderName), refSchema, refName, key)
	}
	switch r.Type {
	case parser.ManyToMany:
		return []string{
			find(r.JunctionSchema, r.JunctionTable, sourceSchema, sourceName, r.JunctionLocal),
			find(r.JunctionSchema, r.JunctionTable, r.TargetSchema, r.TargetTable, r.JunctionReference),
		}
	case parser.OneToMany, parser.OneToOne:
		// An O2M key, and a has-one O2O key, is on the target and references
		// this table; a belongs-to key references the target.
		holderSchema, holderName, refSchema, refName := sourceSchema, sourceName, r.TargetSchema, r.TargetTable
		if r.Type == parser.OneToMany || r.FKOnTarget {
			holderSchema, holderName, refSchema, refName = r.TargetSchema, r.TargetTable, sourceSchema, sourceName
		}
		return []string{find(holderSchema, holderName, refSchema, refName, r.Key)}
	}
	return nil
}

// foreignKeyOver names holder's FOREIGN KEY into refName whose column pairs
// equal key, or "" when it has none.
func foreignKeyOver(holder *parser.Table, refSchema, refName string, key []parser.KeyPair) string {
	if holder == nil || len(key) == 0 {
		return ""
	}
	var match *parser.Constraint
	for i := range holder.Constraints {
		c := &holder.Constraints[i]
		if c.Type != parser.ForeignKey || !referencesTable(*c, refSchema, refName) || len(c.Columns) != len(c.ReferenceColumns) {
			continue
		}
		pairs := make([]parser.KeyPair, len(c.Columns))
		for k := range c.Columns {
			pairs[k] = parser.KeyPair{Local: c.Columns[k], Referenced: c.ReferenceColumns[k]}
		}
		if samePairSet(pairs, key) && (match == nil || c.Name < match.Name) {
			match = c
		}
	}
	if match == nil {
		return ""
	}
	return parser.ForeignKeyName(holder, match)
}

// sameKeyInferred returns the index of the unreplaced detected edge a declared
// edge is: the same kind and target over the same key pair set, and for M2M
// the same junction and both keys. A `filter:` or `discriminator:` edge is a
// sub-category of the detected edge, never the edge itself. -1 means none.
func sameKeyInferred(decl RelationshipContext, inferred []RelationshipContext, replaced map[int]bool) int {
	if decl.Filter != "" || decl.Discriminator != nil {
		return -1
	}
	for i, inf := range inferred {
		if replaced[i] || inf.Type != decl.Type || inf.TargetSchema != decl.TargetSchema || inf.TargetTable != decl.TargetTable {
			continue
		}
		if decl.Type == parser.ManyToMany {
			if inf.JunctionSchema == decl.JunctionSchema && inf.JunctionTable == decl.JunctionTable &&
				samePairSet(inf.JunctionLocal, decl.JunctionLocal) && samePairSet(inf.JunctionReference, decl.JunctionReference) {
				return i
			}
			continue
		}
		if inf.FKOnTarget == decl.FKOnTarget && samePairSet(inf.Key, decl.Key) {
			return i
		}
	}
	return -1
}

// samePairSet reports whether a and b hold the same column pairs, in any order.
func samePairSet(a, b []parser.KeyPair) bool {
	return len(a) > 0 && len(a) == len(b) && !slices.ContainsFunc(a, func(p parser.KeyPair) bool { return !slices.Contains(b, p) })
}
