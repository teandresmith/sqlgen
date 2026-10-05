package gen

import (
	"fmt"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// This file models the identifiers the templates already declare on the
// structs that carry per-column and per-relationship fields, so a resolved Go
// field name that would redeclare one is rejected as a config error instead of
// as an opaque duplicate-field / duplicate-method compile error inside
// generated code (PRD §8.5).
//
// Unlike the duplicate-name half of the rule (column vs column, column vs
// relationship — see validateResolvedFieldNames), this check is *not* gated on
// an override. A reserved-name hit can never produce compiling code, whether
// the name came from `column_map.<col>.name` or straight from the SQL column,
// so refusing it always is strictly more informative than letting `go build`
// report it from a file the user did not write.
//
// The members are not uniformly hazardous: each is claimed only on the struct
// the field actually lands on. `Input` is a field on `Update<T>Item` alongside
// the *single* PK column, so a non-PK column named `input` is fine and must
// keep generating. Conditioning therefore tracks the structural shape of the
// entity — and deliberately stops there. It does not consult the operations
// mask: a name is reserved against the full generated surface, so enabling
// `update_many` or `increment` later never turns a valid config invalid.

// memberScope describes the position a resolved field name occupies, which
// decides which generated members can collide with it.
type memberScope struct {
	// IsColumn distinguishes a column field from a relationship field.
	// Relationships land on the entity struct and on <T>FieldOptions; they
	// never reach <T>Filter, Update<T>Item or the increment enum.
	IsColumn bool
	// Filterable reports whether the column gets a <T>Filter field
	// (columnIsFilterable — a slice of non-comparable elements does not, and
	// neither does a JSON column on SQLite).
	Filterable bool
	// CompositePK reports whether the owning entity has a multi-column PK,
	// which is what emits <T>Filter.PKs.
	CompositePK bool
	// SinglePKColumn reports whether this column is the sole PK column, which
	// is the one that shares Update<T>Item with the Input field.
	SinglePKColumn bool
	// IncrementEligible reports whether the column is enrolled in the
	// increment surface, which emits a <T>Increment<Field> constant.
	IncrementEligible bool
}

// generatedMember is one identifier the generated code already declares, the
// declaration that claims it, and the scopes in which the claim applies.
type generatedMember struct {
	name string
	// owner renders the claiming declaration for the error message, given the
	// entity's Go struct name (e.g. "EventFieldOptions (method Columns)").
	owner func(structName string) string
	// applies reports whether this member is emitted for a field in the given
	// scope. A member that is not emitted cannot collide.
	applies func(memberScope) bool
}

// generatedEntityMembers is the full set of identifiers that a column or
// relationship field name may not equal. Every entry is pinned against the
// templates by TestGeneratedEntityMembers_MatchTemplates, which renders the
// struct-declaring templates with a probe field and derives the same set from
// the emitted Go via go/ast.
var generatedEntityMembers = []generatedMember{
	// <T>FieldOptions carries every column and every relationship, so its
	// three methods are claimed against both. Stream<T>FieldOptions declares
	// Columns and HasSelectedColumns over the same columns.
	{
		name:    "Columns",
		owner:   func(s string) string { return s + "FieldOptions (method Columns)" },
		applies: func(memberScope) bool { return true },
	},
	{
		name:    "ColumnMap",
		owner:   func(s string) string { return s + "FieldOptions (method ColumnMap)" },
		applies: func(memberScope) bool { return true },
	},
	{
		name:    "HasSelectedColumns",
		owner:   func(s string) string { return s + "FieldOptions (method HasSelectedColumns)" },
		applies: func(memberScope) bool { return true },
	},

	// <T>Filter carries only the filterable columns.
	{
		name:    "And",
		owner:   func(s string) string { return s + "Filter (field And)" },
		applies: func(sc memberScope) bool { return sc.IsColumn && sc.Filterable },
	},
	{
		name:    "Or",
		owner:   func(s string) string { return s + "Filter (field Or)" },
		applies: func(sc memberScope) bool { return sc.IsColumn && sc.Filterable },
	},
	{
		name:    "ToConditions",
		owner:   func(s string) string { return s + "Filter (method ToConditions)" },
		applies: func(sc memberScope) bool { return sc.IsColumn && sc.Filterable },
	},
	{
		name:    "PKs",
		owner:   func(s string) string { return s + "Filter (field PKs)" },
		applies: func(sc memberScope) bool { return sc.IsColumn && sc.Filterable && sc.CompositePK },
	},

	// Update<T>Item pairs the single PK column's field with an Input field.
	// A composite PK collapses to a `PK` field instead, so nothing collides.
	{
		name:    "Input",
		owner:   func(s string) string { return "Update" + s + "Item (field Input)" },
		applies: func(sc memberScope) bool { return sc.SinglePKColumn },
	},

	// Package scope rather than a struct member: the per-column constant
	// <T>Increment<Field> would redeclare the <T>IncrementColumn type itself.
	{
		name:    "Column",
		owner:   func(s string) string { return s + "IncrementColumn (the increment enum type)" },
		applies: func(sc memberScope) bool { return sc.IncrementEligible },
	},
}

// reservedMemberFor returns the generated declaration claiming fieldName in
// the given scope, or "" when the name is free.
func reservedMemberFor(fieldName, structName string, scope memberScope) string {
	for _, m := range generatedEntityMembers {
		if m.name == fieldName && m.applies(scope) {
			return m.owner(structName)
		}
	}
	return ""
}

// columnMemberScope derives the scope of a table column.
func columnMemberScope(col ColumnContext, pkColumns []ColumnContext, dialect config.Dialect) memberScope {
	return memberScope{
		IsColumn:          true,
		Filterable:        columnIsFilterable(col, dialect),
		CompositePK:       len(pkColumns) > 1,
		SinglePKColumn:    len(pkColumns) == 1 && col.PrimaryKey,
		IncrementEligible: isIncrementEligible(col),
	}
}

// viewColumnMemberScope derives the scope of a view column. A view reaches
// <V>Filter and <V>FieldOptions through the same two shared templates and
// nothing else: there is no Update<V>Item and no increment enum.
func viewColumnMemberScope(col ColumnContext, pkColumns []ColumnContext, dialect config.Dialect) memberScope {
	return memberScope{
		IsColumn:    true,
		Filterable:  columnIsFilterable(col, dialect),
		CompositePK: len(pkColumns) > 1,
	}
}

// relationshipMemberScope derives the scope of a relationship field. It lands
// on the entity struct and on <T>FieldOptions only.
func relationshipMemberScope() memberScope {
	return memberScope{}
}

// reservedColumnFieldError formats the rejection for a table column, naming
// the per-column escape hatch.
func reservedColumnFieldError(qualifiedTable, structName string, col ColumnContext, scope memberScope) error {
	owner := reservedMemberFor(col.FieldName, structName, scope)
	if owner == "" {
		return nil
	}
	return fmt.Errorf(
		"tables.%s: column %q resolves to Go field name %q, which the generated %s already declares — set tables.%s.column_map.%s.name to a different exported identifier",
		qualifiedTable, col.Name, col.FieldName, owner, qualifiedTable, col.Name,
	)
}

// reservedRelationshipFieldError formats the rejection for a relationship
// field. Relationship names are derived, not configurable, so the escape
// hatch is exclusion rather than a rename.
func reservedRelationshipFieldError(qualifiedTable, structName string, rel RelationshipContext) error {
	owner := reservedMemberFor(rel.FieldName, structName, relationshipMemberScope())
	if owner == "" {
		return nil
	}
	return fmt.Errorf(
		"tables.%s: relationship %q resolves to Go field name %q, which the generated %s already declares — drop it via tables.%s.exclude_relationships",
		qualifiedTable, rel.Name, rel.FieldName, owner, qualifiedTable,
	)
}

// reservedViewFieldError formats the rejection for a view column. Views take
// neither column_map nor exclude_columns, so the only escape is the view
// definition itself.
func reservedViewFieldError(qualifiedView, structName string, col ColumnContext, scope memberScope) error {
	owner := reservedMemberFor(col.FieldName, structName, scope)
	if owner == "" {
		return nil
	}
	return fmt.Errorf(
		"views.%s: column %q resolves to Go field name %q, which the generated %s already declares — rename the column in the view definition (SELECT ... AS ...)",
		qualifiedView, col.Name, col.FieldName, owner,
	)
}
