package parser

import "strings"

// DetectRelationships detects relationships from foreign key constraints in the
// schema. It identifies Many-to-Many (junction tables with exactly 2 FKs in a
// composite PK/UNIQUE), One-to-One (FK column with UNIQUE), and One-to-Many
// (FK column without UNIQUE, the default fallback). Detected relationships are
// appended to schema.Relationships.
func DetectRelationships(schema *Schema) {
	for i := range schema.Tables {
		table := &schema.Tables[i]
		fkCols := fkColumns(table)
		if len(fkCols) == 0 {
			continue
		}

		// M2M: exactly 2 FK columns, both in a composite PK or UNIQUE.
		if len(fkCols) == 2 && junctionConstraint(table, fkCols) {
			addM2M(schema, table, fkCols)
			continue
		}

		// O2O / O2M per FK column.
		for _, col := range fkCols {
			if columnUnique(table, col) {
				addO2O(schema, table, col)
			} else {
				addO2M(schema, table, col)
			}
		}
	}

	disambiguateRelationshipNames(schema)
}

// fkColumns returns all columns in the table that have a foreign key reference.
func fkColumns(table *Table) []Column {
	var cols []Column
	for _, c := range table.Columns {
		if c.FKReference != nil {
			cols = append(cols, c)
		}
	}
	return cols
}

// junctionConstraint reports whether both FK columns appear in a composite
// PRIMARY KEY or UNIQUE constraint on the table. Partial UNIQUE constraints
// (Where != "") are ignored: their uniqueness applies only to rows matching
// the predicate, so they cannot anchor an M2M junction detection.
func junctionConstraint(table *Table, fkCols []Column) bool {
	fkNames := map[string]bool{
		fkCols[0].Name: true,
		fkCols[1].Name: true,
	}

	for _, c := range table.Constraints {
		if c.Type != PrimaryKey && c.Type != Unique {
			continue
		}
		if c.Type == Unique && c.Where != "" {
			continue
		}
		if len(c.Columns) < 2 {
			continue
		}
		matched := 0
		for _, name := range c.Columns {
			if fkNames[name] {
				matched++
			}
		}
		if matched == 2 {
			return true
		}
	}
	return false
}

// columnUnique reports whether the column is unique, either via the Column.Unique
// flag (inline UNIQUE), by being the table's sole primary-key column, or by
// being the sole column in a table-level UNIQUE constraint. Partial UNIQUE
// constraints (Where != "") are ignored: a column constrained only within a
// predicate is not unique enough to promote an FK edge from O2M to O2O.
func columnUnique(table *Table, col Column) bool {
	if col.Unique {
		return true
	}
	// A single-column PRIMARY KEY is implicitly unique on every dialect, so a
	// 1:1 extension table (`profiles(user_id ... PRIMARY KEY REFERENCES
	// users(id))`) holds at most one row per parent and its edge is O2O. A
	// member of a *composite* key carries no such guarantee on its own, which
	// is why this tests the whole key rather than the column's flag — an M2M
	// junction's FK columns must keep falling through to the O2M arm.
	//
	// This clause assumes every FKReference it sees is schema-declared, which
	// holds only because DetectRelationships runs before the CLI hands the
	// schema to gen — gen's applyConfigDeclaredFKs stamps *synthetic*
	// references onto the same schema afterwards, and one landing on a
	// sole-PK column would be promoted to a spurious O2O here. Re-running
	// detection after context building would break that; don't.
	//
	// It makes no such assumption about PrimaryKey. The CLI resolves
	// tables.<name>.primary_key.columns onto these flags immediately before
	// calling this (cli.applyPrimaryKeyOverrides), so an override-declared
	// sole key is unique here exactly as a schema-declared one is — which is
	// what PRD §8.6 means by "functionally identical to a real PRIMARY KEY
	// constraint", and what §13.1 means by "the table's sole PRIMARY KEY
	// column".
	if col.PrimaryKey && singleColumnPK(table) {
		return true
	}
	for _, c := range table.Constraints {
		if c.Type == Unique && c.Where == "" && len(c.Columns) == 1 && c.Columns[0] == col.Name {
			return true
		}
	}
	return false
}

// singleColumnPK reports whether exactly one column on the table is marked
// PrimaryKey. Composite keys are declared by flagging every member column, so
// counting is the only way to tell the two apart.
func singleColumnPK(table *Table) bool {
	n := 0
	for _, c := range table.Columns {
		if c.PrimaryKey {
			n++
			if n > 1 {
				return false
			}
		}
	}
	return n == 1
}

// addM2M creates bidirectional Many-to-Many relationships through a junction table.
func addM2M(schema *Schema, junction *Table, fkCols []Column) {
	jqn := qualifiedTableName(junction.Schema, junction.Name)

	ref0 := fkCols[0].FKReference
	ref1 := fkCols[1].FKReference
	target0 := qualifiedTableName(ref0.Schema, ref0.Table)
	target1 := qualifiedTableName(ref1.Schema, ref1.Table)

	// Forward: target0 -> target1 through junction.
	schema.Relationships = append(schema.Relationships, Relationship{
		Name:                ref1.Table,
		BaseName:            ref1.Table,
		Type:                ManyToMany,
		Side:                SideParent,
		SourceTable:         target0,
		TargetTable:         target1,
		JunctionTable:       jqn,
		JunctionLocalFK:     fkCols[0].Name,
		JunctionReferenceFK: fkCols[1].Name,
	})

	// Reverse: target1 -> target0 through junction.
	schema.Relationships = append(schema.Relationships, Relationship{
		Name:                ref0.Table,
		BaseName:            ref0.Table,
		Type:                ManyToMany,
		Side:                SideParent,
		SourceTable:         target1,
		TargetTable:         target0,
		JunctionTable:       jqn,
		JunctionLocalFK:     fkCols[1].Name,
		JunctionReferenceFK: fkCols[0].Name,
	})
}

// addO2O creates a One-to-One relationship. The source is the table holding the
// FK column, the target is the referenced table.
func addO2O(schema *Schema, table *Table, col Column) {
	ref := col.FKReference
	schema.Relationships = append(schema.Relationships, Relationship{
		Name:        ref.Table,
		BaseName:    ref.Table,
		Type:        OneToOne,
		Side:        SideParent,
		SourceTable: qualifiedTableName(table.Schema, table.Name),
		TargetTable: qualifiedTableName(ref.Schema, ref.Table),
		FKColumn:    col.Name,
	})
}

// addO2M creates a One-to-Many relationship. The source is the referenced (one)
// table, the target is the FK-holding (many) table.
func addO2M(schema *Schema, table *Table, col Column) {
	ref := col.FKReference
	schema.Relationships = append(schema.Relationships, Relationship{
		Name:        table.Name,
		BaseName:    table.Name,
		Type:        OneToMany,
		Side:        SideParent,
		SourceTable: qualifiedTableName(ref.Schema, ref.Table),
		TargetTable: qualifiedTableName(table.Schema, table.Name),
		FKColumn:    col.Name,
	})
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
// convention per relationship type:
//
//   - O2M: "<fk-stem>_<base>" so the generated field reads as
//     "<FkStem><Base>" (e.g. "developer_asset" → "DeveloperAsset" — "the
//     asset I am the developer of"). The role qualifier comes first so the
//     target type-noun anchors the read.
//   - O2O: "<fk-stem>" alone. The FK column is itself the natural
//     identifier (e.g. "primary_profile") and the Go target type is visible
//     on the field declaration.
//   - M2M: junction table base name (e.g. "team_members" → "TeamMembers")
//     so a multi-junction shape (team_members + team_managers, both joining
//     team↔users) renders as Team.TeamMembers / Team.TeamManagers rather
//     than UsersTeamMembers / UsersTeamManagers.
//
// Single-edge groups are left untouched so existing schemas keep their
// current names and downstream goldens stay byte-stable.
func disambiguateRelationshipNames(schema *Schema) {
	type key struct {
		source string
		target string
	}
	groups := make(map[key][]int)
	for i, r := range schema.Relationships {
		k := key{source: r.SourceTable, target: r.TargetTable}
		groups[k] = append(groups[k], i)
	}
	for _, idxs := range groups {
		if len(idxs) < 2 {
			continue
		}
		for _, idx := range idxs {
			r := &schema.Relationships[idx]
			newName := disambiguatedRelationshipName(*r)
			if newName == "" {
				continue
			}
			r.Name = newName
		}
	}
}

// disambiguatedRelationshipName computes the rewritten Name for a colliding
// relationship. See disambiguateRelationshipNames for the per-type
// convention.
func disambiguatedRelationshipName(r Relationship) string {
	switch r.Type {
	case OneToMany:
		stem := strings.TrimSuffix(r.FKColumn, "_id")
		if stem == "" {
			return ""
		}
		return stem + "_" + r.BaseName
	case OneToOne:
		return strings.TrimSuffix(r.FKColumn, "_id")
	case ManyToMany:
		// Self-referential M2M (both junction FKs target the same table, so
		// SourceTable == TargetTable): naming both directions after the shared
		// junction table would collide and emit two identically-named fields.
		// Derive each direction's name from its own reference FK column instead
		// — the two FK columns are distinct by construction (e.g. task_id vs
		// depends_on_task_id → Tasks vs DependsOnTasks), so the names differ.
		// Users can still rename or exclude+redeclare as with any auto edge.
		if r.SourceTable == r.TargetTable && r.JunctionReferenceFK != "" {
			return strings.TrimSuffix(r.JunctionReferenceFK, "_id")
		}
		_, name := splitQualifiedRelationshipTable(r.JunctionTable)
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
