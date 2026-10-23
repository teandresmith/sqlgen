package parser

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// DetectRelationships replaces schema.Relationships with the edges its
// FOREIGN KEY constraints give (PRD §5.3), so running it twice changes
// nothing. Each FOREIGN KEY gives at most one edge: an M2M edge when its table
// is a junction, else an O2O edge when a key of its table lies within its
// columns, else an O2M edge.
//
// Run it after ResolveReferenceNames and the primary-key overrides: it compares
// referenced columns with the target's spelling and reads the resolved primary
// key off the column flags.
//
// It returns one warning per foreign key whose referenced columns are not a
// key of the target, since such a key gives no edge.
func DetectRelationships(schema *Schema) []string {
	var (
		edges    []detectedEdge
		warnings []string
	)
	for i := range schema.Tables {
		table := &schema.Tables[i]
		fks, tableWarnings := foreignKeys(schema, table)
		warnings = append(warnings, tableWarnings...)
		if len(fks) == 0 {
			continue
		}
		if a, b, ok := junctionKeys(table, fks); ok {
			// A junction gives only its M2M edge, so one with an end that
			// gives no edge gives none.
			if !a.noEdge && !b.noEdge {
				edges = append(edges, m2mEdges(table, a, b, fks)...)
			}
			continue
		}
		for _, fk := range fks {
			if !fk.noEdge {
				edges = append(edges, keyEdge(table, fk, fks))
			}
		}
	}

	disambiguateRelationshipNames(edges)
	schema.Relationships = make([]Relationship, len(edges))
	for i, e := range edges {
		schema.Relationships[i] = e.Relationship
	}
	return warnings
}

// detectedEdge is a relationship with the member column its name is derived
// from when it collides (PRD §5.3 rule 5): the naming member of an O2O / O2M
// edge's key, or of an M2M edge's reference key. Empty when there is none.
type detectedEdge struct {
	Relationship
	namingMember string
}

// foreignKey is a FOREIGN KEY of a table. noEdge marks one whose target is not
// in the schema or whose referenced columns are not a key of it: it gives no
// edge, but it still counts when deciding whether its table is a junction.
type foreignKey struct {
	constraint *Constraint
	target     *Table
	key        []KeyPair
	name       string
	positions  []int // the local columns' positions in the table, for ordering
	noEdge     bool
	declared   int // the index in Table.Constraints
}

func (fk foreignKey) localColumns() []string { return fk.constraint.Columns }

// foreignKeys returns the table's foreign keys less the redundant ones (PRD
// §5.3 rules 1 and 2), ordered by the positions of their columns.
func foreignKeys(schema *Schema, table *Table) ([]foreignKey, []string) {
	var (
		fks      []foreignKey
		warnings []string
	)
	for j := range table.Constraints {
		c := &table.Constraints[j]
		if c.Type != ForeignKey {
			continue
		}
		fk := foreignKey{constraint: c, name: foreignKeyName(table, c), declared: j}
		for _, name := range c.Columns {
			fk.positions = append(fk.positions, slices.IndexFunc(table.Columns, func(col Column) bool { return col.Name == name }))
		}
		fk.target = findTable(schema, c.ReferenceSchema, c.ReferenceTable)
		switch {
		case fk.target == nil:
			// A key into a table the schema does not hold gives no edge (PRD §5.5).
			fk.noEdge = true
		case len(c.ReferenceColumns) != len(c.Columns) || !isKey(tableKeys(fk.target, true, true), c.ReferenceColumns):
			fk.noEdge = true
			warnings = append(warnings, fmt.Sprintf(
				"tables.%s: foreign key %s references columns that are neither the primary key nor a non-partial UNIQUE constraint of %s, so it gives no relationship; declare one by hand if you need it (PRD §5.3, §13.4)",
				qualifiedTableName(table.Schema, table.Name), fk.name, qualifiedTableName(fk.target.Schema, fk.target.Name),
			))
		default:
			fk.key = make([]KeyPair, len(c.Columns))
			for k := range c.Columns {
				fk.key[k] = KeyPair{Local: c.Columns[k], Referenced: c.ReferenceColumns[k]}
			}
		}
		fks = append(fks, fk)
	}
	slices.SortStableFunc(fks, func(a, b foreignKey) int {
		return slices.Compare(a.positions, b.positions)
	})
	return withoutRedundant(fks), warnings
}

// withoutRedundant applies PRD §5.3 rule 2: a key whose pairs lie within
// another's to the same target is redundant. Of identical keys, whatever
// order each lists its columns in, the one whose name sorts first is kept, so
// every source keeps the same one: the introspectors list constraints by
// name. Unnamed keys (SQLite) keep the first declared.
func withoutRedundant(fks []foreignKey) []foreignKey {
	var kept []foreignKey
	for i, fk := range fks {
		redundant := false
		for j, o := range fks {
			if j != i && !fk.noEdge && !o.noEdge && o.target == fk.target && within(fk.key, o.key) && (len(fk.key) < len(o.key) || firstOfIdentical(o, fk)) {
				redundant = true
				break
			}
		}
		if !redundant {
			kept = append(kept, fk)
		}
	}
	return kept
}

// firstOfIdentical reports whether a is kept over b, an identical key.
func firstOfIdentical(a, b foreignKey) bool {
	return cmp.Or(strings.Compare(a.constraint.Name, b.constraint.Name), cmp.Compare(a.declared, b.declared)) < 0
}

// foreignKeyName is the constraint's name, or "table(columns) →
// target(columns)" for an unnamed one, as SQLite's usually are.
func foreignKeyName(table *Table, c *Constraint) string {
	if c.Name != "" {
		return c.Name
	}
	return fmt.Sprintf("%s(%s) → %s(%s)", table.Name, strings.Join(c.Columns, ", "), c.ReferenceTable, strings.Join(c.ReferenceColumns, ", "))
}

func findTable(schema *Schema, tableSchema, name string) *Table {
	for i := range schema.Tables {
		if t := &schema.Tables[i]; t.Schema == tableSchema && t.Name == name {
			return t
		}
	}
	return nil
}

// resolvedPK returns the table's primary-key columns in column order. The
// column flags carry any primary_key override.
func resolvedPK(table *Table) []string {
	var pk []string
	for _, col := range table.Columns {
		if col.PrimaryKey {
			pk = append(pk, col.Name)
		}
	}
	return pk
}

// tableKeys returns the column sets the table holds unique: every non-partial
// UNIQUE (constraint or column flag), plus its resolved primary key when
// resolved is set and its declared PRIMARY KEY constraint when declared is
// set. The database enforces the declared key even when an override moves the
// primary key, and an override is the table's key for everything sqlgen
// generates (PRD §8.6).
func tableKeys(table *Table, resolved, declared bool) [][]string {
	var keys [][]string
	if pk := resolvedPK(table); resolved && len(pk) > 0 {
		keys = append(keys, pk)
	}
	for _, c := range table.Constraints {
		unique := c.Type == Unique && c.Where == "" || c.Type == PrimaryKey && declared
		if unique && len(c.Columns) > 0 {
			keys = append(keys, c.Columns)
		}
	}
	for _, col := range table.Columns {
		if col.Unique {
			keys = append(keys, []string{col.Name})
		}
	}
	return keys
}

// isKey reports whether cols, as a set, is one of keys.
func isKey(keys [][]string, cols []string) bool {
	return slices.ContainsFunc(keys, func(k []string) bool {
		return len(k) == len(cols) && within(k, cols)
	})
}

// within reports whether every element of a is in b.
func within[T comparable](a, b []T) bool {
	for _, x := range a {
		if !slices.Contains(b, x) {
			return false
		}
	}
	return true
}

// junctionKeys applies PRD §5.3 rule 4. It sets aside every foreign key whose
// columns all belong to the table's other foreign keys; the table is a
// junction when exactly two remain and a PRIMARY KEY or non-partial UNIQUE
// constraint contains their columns. A primary_key override is not such a constraint, so
// declaring one over two foreign keys does not make their table a junction.
func junctionKeys(table *Table, fks []foreignKey) (foreignKey, foreignKey, bool) {
	var remain []foreignKey
	for i, fk := range fks {
		var others []string
		for j, o := range fks {
			if j != i {
				others = append(others, o.localColumns()...)
			}
		}
		if !within(fk.localColumns(), others) {
			remain = append(remain, fk)
		}
	}
	if len(remain) != 2 {
		return foreignKey{}, foreignKey{}, false
	}
	union := slices.Clone(remain[0].localColumns())
	for _, col := range remain[1].localColumns() {
		if !slices.Contains(union, col) {
			union = append(union, col)
		}
	}
	if len(union) < 2 {
		return foreignKey{}, foreignKey{}, false
	}
	for _, k := range tableKeys(table, false, true) {
		if within(union, k) {
			return remain[0], remain[1], true
		}
	}
	return foreignKey{}, foreignKey{}, false
}

// keyEdge returns the O2O or O2M edge of a foreign key on a table that is not
// a junction (PRD §5.3 rule 3). It is O2O when one of the table's keys lies
// within the key's columns, which makes the whole key unique.
func keyEdge(table *Table, fk foreignKey, fks []foreignKey) detectedEdge {
	holder := qualifiedTableName(table.Schema, table.Name)
	target := qualifiedTableName(fk.target.Schema, fk.target.Name)
	e := detectedEdge{
		Relationship: Relationship{
			Name:        table.Name,
			BaseName:    table.Name,
			Type:        OneToMany,
			Side:        SideParent,
			SourceTable: target,
			TargetTable: holder,
			Constraint:  fk.name,
			Key:         fk.key,
		},
		namingMember: namingMember(fk, fks, resolvedPK(table)),
	}
	cols := fk.localColumns()
	if slices.ContainsFunc(tableKeys(table, true, false), func(k []string) bool { return within(k, cols) }) {
		e.Name, e.BaseName, e.Type = fk.target.Name, fk.target.Name, OneToOne
		e.SourceTable, e.TargetTable = holder, target
	}
	return e
}

// m2mEdges returns both directions of the M2M edge through junction.
func m2mEdges(junction *Table, a, b foreignKey, fks []foreignKey) []detectedEdge {
	jqn := qualifiedTableName(junction.Schema, junction.Name)
	edge := func(local, ref foreignKey) detectedEdge {
		return detectedEdge{
			Relationship: Relationship{
				Name:                        ref.target.Name,
				BaseName:                    ref.target.Name,
				Type:                        ManyToMany,
				Side:                        SideParent,
				SourceTable:                 qualifiedTableName(local.target.Schema, local.target.Name),
				TargetTable:                 qualifiedTableName(ref.target.Schema, ref.target.Name),
				JunctionTable:               jqn,
				JunctionLocal:               local.key,
				JunctionReference:           ref.key,
				JunctionLocalConstraint:     local.name,
				JunctionReferenceConstraint: ref.name,
			},
			namingMember: namingMember(ref, fks, resolvedPK(junction)),
		}
	}
	return []detectedEdge{edge(a, b), edge(b, a)}
}

// namingMember returns the key member an edge's name is derived from (PRD
// §5.3 rule 5), the first of these that is exactly one column:
//  1. the member neither in the table's primary key pk nor in another of its
//     foreign keys;
//  2. the member in no other foreign key;
//  3. the member paired with the target's single-column primary key.
//
// Otherwise it returns "". The other foreign keys are fks, the table's keys
// less the redundant ones: a duplicate of the key, or a key inside it, does
// not take its naming member. For a single-column key that column is the
// naming member whenever it is the target's primary key or in no other
// foreign key.
func namingMember(fk foreignKey, fks []foreignKey, pk []string) string {
	shared := func(col string) bool {
		return slices.ContainsFunc(fks, func(o foreignKey) bool {
			return o.constraint != fk.constraint && slices.Contains(o.constraint.Columns, col)
		})
	}
	var free, unshared []string
	for _, p := range fk.key {
		if shared(p.Local) {
			continue
		}
		unshared = append(unshared, p.Local)
		if !slices.Contains(pk, p.Local) {
			free = append(free, p.Local)
		}
	}
	switch {
	case len(free) == 1:
		return free[0]
	case len(unshared) == 1:
		return unshared[0]
	}
	if tpk := resolvedPK(fk.target); len(tpk) == 1 {
		for _, p := range fk.key {
			if p.Referenced == tpk[0] {
				return p.Local
			}
		}
	}
	return ""
}

// qualifiedTableName returns "schema.name" when schema is non-empty, or just "name".
func qualifiedTableName(schema, name string) string {
	if schema == "" {
		return name
	}
	return schema + "." + name
}

// disambiguateRelationshipNames walks the emitted relationships and resolves
// Name collisions caused by multiple edges between the same source/target
// pair — either same-kind (e.g. an asset table with developer_id,
// om_provider_id, owner_id all referencing stakeholder) or cross-kind (an
// organizations parent with both an M2M to users through organization_users
// AND an O2M to users via users.workspace_id). Without disambiguation,
// every inverse-side relationship collapses to duplicate parent-struct
// fields in generated code.
//
// Grouping key is (SourceTable, TargetTable) — kind-agnostic, so cross-kind
// collisions like the M2M + O2M case above are detected. Each member of a
// collision group has its Name rewritten to a unique edge-identifying form
// derived from its own metadata; BaseName is preserved across the rewrite
// so `exclude_relationships` entries written against the bare table name
// continue to match every disambiguated edge in the group. Naming
// convention per relationship type, where <stem> is the naming member
// without its `_id` suffix:
//
//   - O2M: "<stem>_<base>" so the generated field reads as
//     "<Stem><Base>" (e.g. "developer_asset" → "DeveloperAsset" — "the
//     asset I am the developer of"). The role qualifier comes first so the
//     target type-noun anchors the read.
//   - O2O: "<stem>" alone. The FK column is itself the natural
//     identifier (e.g. "primary_profile") and the Go target type is visible
//     on the field declaration.
//   - M2M: junction table base name (e.g. "team_members" → "TeamMembers")
//     so a multi-junction shape (team_members + team_managers, both joining
//     team↔users) renders as Team.TeamMembers / Team.TeamManagers rather
//     than UsersTeamMembers / UsersTeamManagers.
//
// An edge with no naming member keeps its name. Single-edge groups are left
// untouched so existing schemas keep their current names and downstream
// goldens stay byte-stable.
func disambiguateRelationshipNames(edges []detectedEdge) {
	type key struct {
		source string
		target string
	}
	groups := make(map[key][]int)
	for i, e := range edges {
		k := key{source: e.SourceTable, target: e.TargetTable}
		groups[k] = append(groups[k], i)
	}
	for _, idxs := range groups {
		if len(idxs) < 2 {
			continue
		}
		for _, idx := range idxs {
			e := &edges[idx]
			if newName := disambiguatedRelationshipName(*e); newName != "" {
				e.Name = newName
			}
		}
	}
}

// disambiguatedRelationshipName computes the rewritten Name for a colliding
// relationship. See disambiguateRelationshipNames for the per-type
// convention.
func disambiguatedRelationshipName(e detectedEdge) string {
	switch e.Type {
	case OneToMany:
		stem := strings.TrimSuffix(e.namingMember, "_id")
		if stem == "" {
			return ""
		}
		return stem + "_" + e.BaseName
	case OneToOne:
		return strings.TrimSuffix(e.namingMember, "_id")
	case ManyToMany:
		// Self-referential M2M (both junction FKs target the same table, so
		// SourceTable == TargetTable): naming both directions after the shared
		// junction table would collide and emit two identically-named fields.
		// Derive each direction's name from its own reference key's naming
		// member instead (e.g. task_id vs depends_on_task_id → Tasks vs
		// DependsOnTasks). Users can still rename or exclude+redeclare as with
		// any auto edge.
		if e.SourceTable == e.TargetTable && e.namingMember != "" {
			return strings.TrimSuffix(e.namingMember, "_id")
		}
		_, name := splitQualifiedRelationshipTable(e.JunctionTable)
		return name
	}
	return ""
}

// splitQualifiedRelationshipTable splits "schema.name" into ("schema", "name").
// When no schema prefix is present, returns ("", input).
func splitQualifiedRelationshipTable(qualified string) (string, string) {
	schema, name, ok := strings.Cut(qualified, ".")
	if !ok {
		return "", qualified
	}
	return schema, name
}
