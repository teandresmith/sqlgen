package parser

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ConstraintType represents the kind of a table constraint.
type ConstraintType int

const (
	// PrimaryKey is a PRIMARY KEY constraint.
	PrimaryKey ConstraintType = iota + 1
	// ForeignKey is a FOREIGN KEY constraint.
	ForeignKey
	// Unique is a UNIQUE constraint.
	Unique
	// Check is a CHECK constraint.
	Check
	// Index is an INDEX constraint.
	Index
)

// String returns the string representation of the constraint type.
func (ct ConstraintType) String() string {
	switch ct {
	case PrimaryKey:
		return "PRIMARY KEY"
	case ForeignKey:
		return "FOREIGN KEY"
	case Unique:
		return "UNIQUE"
	case Check:
		return "CHECK"
	case Index:
		return "INDEX"
	default:
		return "UNKNOWN"
	}
}

// RelationshipType represents the cardinality of a relationship.
type RelationshipType int

const (
	// OneToOne is a one-to-one relationship.
	OneToOne RelationshipType = iota + 1
	// OneToMany is a one-to-many relationship.
	OneToMany
	// ManyToMany is a many-to-many relationship.
	ManyToMany
)

// String returns the string representation of the relationship type.
func (rt RelationshipType) String() string {
	switch rt {
	case OneToOne:
		return "OneToOne"
	case OneToMany:
		return "OneToMany"
	case ManyToMany:
		return "ManyToMany"
	default:
		return "Unknown"
	}
}

// RelationshipSide identifies which end of a relationship edge this entry
// represents. For O2O / O2M / M2O, "parent" is the FK-holder (source of the
// directed edge) and "child" is the referenced side. For M2M, both peers are
// "parent" — the cardinality is symmetric.
type RelationshipSide int

const (
	// SideUnspecified is the zero value. Production emit paths
	// (DetectRelationships' addO2O / addO2M / addM2M, plus the codegen's
	// config-relationship builder) always populate Side, so consumers that
	// observe SideUnspecified are reading a hand-built fixture that
	// bypassed those builders — surface that as "no signal" rather than
	// guessing.
	SideUnspecified RelationshipSide = iota
	// SideParent marks the FK-holder end of an O2O / O2M / M2O edge, or
	// either peer of an M2M edge.
	SideParent
	// SideChild marks the referenced end of an O2O / O2M / M2O edge.
	SideChild
)

// String returns the string representation of the relationship side.
func (s RelationshipSide) String() string {
	switch s {
	case SideParent:
		return "parent"
	case SideChild:
		return "child"
	default:
		return "unspecified"
	}
}

// Schema is the top-level container for a parsed SQL schema.
type Schema struct {
	Tables         []Table
	Enums          []Enum
	Sets           []Set
	CompositeTypes []CompositeType
	DomainTypes    []DomainType
	Views          []View
	Relationships  []Relationship
	Warnings       []string

	// Partitions lists the PostgreSQL partitions both sources skip (PRD
	// §5.2, §6.3). A partition is not an entity, but PostgreSQL can index
	// it, so under source: both the file parser needs to know the
	// database's partitions to accept a CREATE INDEX on one.
	Partitions []Partition
}

// Partition is one PostgreSQL partition (CREATE TABLE … PARTITION OF) and the
// table it is a partition of.
type Partition struct {
	Schema       string
	Name         string
	ParentSchema string
	ParentName   string
}

// Sort sorts all elements in the schema deterministically.
// Tables, enums, composite types, domain types, and views sort by schema then name.
// Columns within tables maintain their insertion order (ordinal position).
// Relationships sort by name.
func (s *Schema) Sort() {
	slices.SortFunc(s.Tables, func(a, b Table) int {
		if c := strings.Compare(a.Schema, b.Schema); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})

	slices.SortFunc(s.Enums, func(a, b Enum) int {
		if c := strings.Compare(a.Schema, b.Schema); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})

	slices.SortFunc(s.Sets, func(a, b Set) int {
		if c := strings.Compare(a.Schema, b.Schema); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})

	slices.SortFunc(s.CompositeTypes, func(a, b CompositeType) int {
		if c := strings.Compare(a.Schema, b.Schema); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})

	slices.SortFunc(s.DomainTypes, func(a, b DomainType) int {
		if c := strings.Compare(a.Schema, b.Schema); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})

	slices.SortFunc(s.Views, func(a, b View) int {
		if c := strings.Compare(a.Schema, b.Schema); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})

	slices.SortFunc(s.Relationships, func(a, b Relationship) int {
		return strings.Compare(a.Name, b.Name)
	})

	slices.SortFunc(s.Partitions, func(a, b Partition) int {
		if c := strings.Compare(a.Schema, b.Schema); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
}

// Table represents a parsed SQL table.
type Table struct {
	Name         string
	Schema       string
	Columns      []Column
	Constraints  []Constraint
	Comment      string
	Strict       bool // SQLite STRICT table modifier (3.37+)
	WithoutRowID bool // SQLite WITHOUT ROWID table modifier (3.8.2+)

	// ExpressionIndexes holds the table's indexes with an expression part,
	// which Constraints leaves out (PostgreSQL and MySQL).
	ExpressionIndexes []ExpressionIndex
}

// Column represents a column in a table.
type Column struct {
	Name             string
	Type             string
	Nullable         bool
	PrimaryKey       bool
	Unique           bool
	Default          string
	FKReference      *FKReference
	AutoIncrement    bool
	Comment          string
	GeneratedExpr    string // Generated column expression (SQLite 3.31+)
	GeneratedStorage string // "VIRTUAL" or "STORED" (default: "VIRTUAL")
	// GoTypeLiteral is set for view columns whose type was resolved to a Go type
	// by @type annotation or aggregate inference. When non-empty, the code generator
	// uses this directly instead of running the SQL-to-Go resolver.
	GoTypeLiteral string
	// GoTypeImport is the import path for an external Go type (from @type annotation).
	GoTypeImport string
	// Aggregate names the SQL aggregate function (uppercase, e.g. "SUM") when this
	// view column is an aggregate whose Go type flows through the SQL-to-Go
	// resolver rather than a Go-type literal. Empty for table columns and for
	// aggregates resolved to a literal. The generator consults it so the
	// resolver can model dialect-specific result widening (e.g. SUM(integer) →
	// bigint in PostgreSQL); see gotype.Resolver.ResolveAggregate.
	Aggregate string
}

// FKReference represents a foreign key reference to another table's column.
type FKReference struct {
	Table  string
	Schema string
	Column string
	// Synthetic marks a reference invented by a config-declared relationship
	// (gen.applyConfigDeclaredFKs) rather than read from a SQL REFERENCES
	// clause. The database does not enforce it, and it may name a column that
	// holds no foreign key at all, so any consumer deciding something about
	// the *referential contract* — as opposed to how a column is filtered or
	// typed — must exclude it. Column is empty for synthetic references;
	// test Synthetic, not Column, to tell the two apart.
	Synthetic bool
}

// Constraint represents a table-level constraint.
//
// Method and Where apply only to index-shaped constraints (Index and Unique).
// Method holds the access-method name: "btree", "gin", "gist", "hash", and so
// on. CanonicalizeConstraints defaults it to "btree" on PostgreSQL and MySQL.
// Where holds the predicate text of a partial index (PostgreSQL / SQLite)
// without the leading WHERE keyword; MySQL does not support partial indexes
// so Where stays empty for that dialect.
type Constraint struct {
	Name             string
	Type             ConstraintType
	Columns          []string
	ReferenceTable   string
	ReferenceSchema  string
	ReferenceColumns []string
	CheckExpression  string
	Method           string
	Where            string
}

// Enum represents a named enum type with ordered values.
type Enum struct {
	Name    string
	Schema  string
	Values  []string
	Comment string
}

// Set represents a MySQL SET type with ordered allowed values.
// Unlike enums, SET columns store zero or more values as a comma-separated string.
type Set struct {
	Name    string
	Schema  string
	Values  []string
	Comment string
}

// Attribute represents a named field with a type, used in composite types.
type Attribute struct {
	Name string
	Type string
}

// CompositeType represents a named struct-like type with attributes.
type CompositeType struct {
	Name       string
	Schema     string
	Attributes []Attribute
	Comment    string
}

// DomainType represents a named alias type with optional constraints.
type DomainType struct {
	Name        string
	Schema      string
	BaseType    string
	Constraints []Constraint
	Comment     string
}

// RelationshipSort defines the ordering for loaded related entities.
type RelationshipSort struct {
	Column    string
	Direction string
}

// Relationship represents a detected or configured relationship between tables.
//
// BaseName is the pre-disambiguation Name produced at emission time
// (DetectRelationships) — the child table for O2M, the target table for O2O,
// the M2M-target table for ManyToMany. When multiple FK edges connect the
// same (SourceTable, TargetTable) pair, Name is rewritten by
// disambiguateRelationshipNames to a unique form derived from the FK column
// (O2M / O2O) or junction table (M2M); BaseName preserves the original so
// `exclude_relationships` entries written against the bare table name (PRD
// §4.8 / §13.4) continue to match every disambiguated edge in the group.
type Relationship struct {
	Name                string
	BaseName            string
	Type                RelationshipType
	Side                RelationshipSide
	SourceTable         string
	TargetTable         string
	FKColumn            string
	JunctionTable       string
	JunctionLocalFK     string
	JunctionReferenceFK string
	Filter              string
	Sort                []RelationshipSort
}

// View represents a SQL view (regular or materialized) with its source SQL and
// inferred columns.
type View struct {
	Name    string
	Schema  string
	SQL     string
	Columns []Column

	// Materialized reports whether this is a PostgreSQL MATERIALIZED VIEW.
	// When true, the generator emits Refresh methods and the FROM clause still
	// targets the relation by name (reads are identical to a regular view).
	Materialized bool

	// ConcurrentlyRefreshable reports whether the matview carries at least one
	// UNIQUE index covering all rows — the prerequisite for
	// REFRESH MATERIALIZED VIEW CONCURRENTLY. Set during introspection; for
	// annotation-file matviews it follows from an @pk directive.
	// Always false for regular views (Materialized == false implies
	// ConcurrentlyRefreshable == false).
	ConcurrentlyRefreshable bool

	// Definition is the parsed definition an introspected view's columns
	// were read from (PRD §16.2), so that source: both can read them again
	// against the schema the SQL files leave. Nil for a view from an
	// annotation file, and for one whose columns come from the catalog.
	Definition *ViewSQL
}

// RenameTableReferences repoints every foreign key that references the table
// (schema, oldName) at (schema, newName), so a RENAME TABLE keeps the keys
// that reference the table resolvable. A reference matches the table as
// dialect matches names (SameTableName).
func RenameTableReferences(tables []Table, dialect, schema, oldName, newName string) {
	retargetTableReferences(tables, dialect, schema, oldName, schema, newName)
}

// MoveTableReferences repoints every foreign key that references the
// PostgreSQL table (oldSchema, name) at (newSchema, name), so an ALTER TABLE …
// SET SCHEMA keeps the keys that reference the table resolvable.
func MoveTableReferences(tables []Table, name, oldSchema, newSchema string) {
	retargetTableReferences(tables, "postgres", oldSchema, name, newSchema, name)
}

func retargetTableReferences(tables []Table, dialect, fromSchema, fromName, toSchema, toName string) {
	for i := range tables {
		t := &tables[i]
		for j := range t.Columns {
			ref := t.Columns[j].FKReference
			if ref != nil && ref.Schema == fromSchema && SameTableName(dialect, ref.Table, fromName) {
				ref.Schema, ref.Table = toSchema, toName
			}
		}
		for j := range t.Constraints {
			c := &t.Constraints[j]
			if c.Type == ForeignKey && c.ReferenceSchema == fromSchema && SameTableName(dialect, c.ReferenceTable, fromName) {
				c.ReferenceSchema, c.ReferenceTable = toSchema, toName
			}
		}
	}
}

// RenameColumnReferences rewrites every reference to column oldName of the
// table (schema, table) to newName: the column lists of that table's own
// constraints (PRIMARY KEY, UNIQUE, FOREIGN KEY, INDEX, CHECK), and every
// foreign key, on any table, that references the column. The database does
// the same on RENAME COLUMN; left on the old name, a composite key no longer
// matches its columns, so an M2M junction degrades to O2M and an upsert
// conflict target names a column that no longer exists. Tables and columns
// match as dialect matches names (SameTableName, SameColumnName), so a
// reference written in another case follows the rename too. Call it after
// RenameColumn has renamed the column itself.
func RenameColumnReferences(tables []Table, dialect, schema, table, oldName, newName string) {
	for i := range tables {
		t := &tables[i]
		owner := t.Schema == schema && SameTableName(dialect, t.Name, table)
		for j := range t.Columns {
			ref := t.Columns[j].FKReference
			if ref != nil && ref.Schema == schema && SameTableName(dialect, ref.Table, table) &&
				SameColumnName(dialect, ref.Column, oldName) {
				ref.Column = newName
			}
		}
		for j := range t.Constraints {
			c := &t.Constraints[j]
			if owner {
				replaceName(c.Columns, dialect, oldName, newName)
			}
			if c.Type == ForeignKey && c.ReferenceSchema == schema && SameTableName(dialect, c.ReferenceTable, table) {
				replaceName(c.ReferenceColumns, dialect, oldName, newName)
			}
		}
	}
}

// replaceName replaces every name in names that names column oldName in
// dialect with newName, in place.
func replaceName(names []string, dialect, oldName, newName string) {
	for i := range names {
		if SameColumnName(dialect, names[i], oldName) {
			names[i] = newName
		}
	}
}

// UnresolvedForeignKeys returns one error per foreign key whose referenced
// table is not in tables, sorted for deterministic output. A reference
// matches a table as dialect matches names (SameTableName). Relationship
// detection builds an edge from every foreign key, and an edge whose far end
// was never parsed is dropped without a trace, so the caller reports these
// instead.
func UnresolvedForeignKeys(tables []Table, dialect string) []error {
	known := make(map[string]bool, len(tables))
	for i := range tables {
		known[qualifiedName(tables[i].Schema, tableKey(dialect, tables[i].Name))] = true
	}
	seen := make(map[string]bool)
	var msgs []string
	add := func(t *Table, cols, refSchema, refTable string) {
		if known[qualifiedName(refSchema, tableKey(dialect, refTable))] {
			return
		}
		msg := fmt.Sprintf("foreign key %s(%s) references %s, which no parsed table defines",
			qualifiedName(t.Schema, t.Name), cols, qualifiedName(refSchema, refTable))
		if !seen[msg] {
			seen[msg] = true
			msgs = append(msgs, msg)
		}
	}
	for i := range tables {
		t := &tables[i]
		for _, col := range t.Columns {
			if col.FKReference != nil {
				add(t, col.Name, col.FKReference.Schema, col.FKReference.Table)
			}
		}
		for _, c := range t.Constraints {
			if c.Type == ForeignKey {
				add(t, strings.Join(c.Columns, ", "), c.ReferenceSchema, c.ReferenceTable)
			}
		}
	}
	slices.Sort(msgs)
	errs := make([]error, 0, len(msgs))
	for _, m := range msgs {
		errs = append(errs, errors.New(m))
	}
	return errs
}

// TableRef names a table by schema and name.
type TableRef struct {
	Schema, Name string
}

// CheckTableDropRefs checks whether the tables in drop, the tables of one DROP
// TABLE statement, can be dropped together: a foreign key from a table outside
// drop to one of them blocks the drop, while a foreign key between tables in
// drop does not. Names match as dialect matches them (SameTableName). Returns
// an error describing the first blocking reference found, or nil if the drop
// is safe.
func CheckTableDropRefs(tables []Table, dialect string, drop []TableRef) error {
	dropping := func(schema, name string) bool {
		return slices.ContainsFunc(drop, func(r TableRef) bool {
			return r.Schema == schema && SameTableName(dialect, r.Name, name)
		})
	}
	for _, ref := range drop {
		for i := range tables {
			t := &tables[i]
			if dropping(t.Schema, t.Name) {
				continue // dropped with it
			}
			for _, col := range t.Columns {
				if col.FKReference != nil && col.FKReference.Schema == ref.Schema && SameTableName(dialect, col.FKReference.Table, ref.Name) {
					return fmt.Errorf(
						"cannot drop table %s: column %s.%s references it via foreign key",
						qualifiedName(ref.Schema, ref.Name), qualifiedName(t.Schema, t.Name), col.Name,
					)
				}
			}
			for _, c := range t.Constraints {
				if c.Type == ForeignKey && c.ReferenceSchema == ref.Schema && SameTableName(dialect, c.ReferenceTable, ref.Name) {
					return fmt.Errorf(
						"cannot drop table %s: table %s has a foreign key constraint referencing it",
						qualifiedName(ref.Schema, ref.Name), qualifiedName(t.Schema, t.Name),
					)
				}
			}
		}
	}
	return nil
}

// TypeRef names a user type, an enum, domain or composite type, by schema
// and name.
type TypeRef struct {
	Schema, Name string
}

// ResolveTypeRef reads a column, attribute or domain base type written for
// schema owner: a bare name is a type in owner, a qualified one names its
// schema, and an array of a type refers to that type.
func ResolveTypeRef(typ, owner string) (ref TypeRef, isArray bool) {
	base, isArray := strings.CutSuffix(typ, "[]")
	if typSchema, typName, ok := strings.Cut(base, "."); ok {
		return TypeRef{Schema: typSchema, Name: typName}, isArray
	}
	return TypeRef{Schema: owner, Name: base}, isArray
}

// CheckTypeDropRefs checks whether the types in drop, the objects of one DROP
// TYPE or DROP DOMAIN statement, can be dropped together. A table or view
// column, a domain's base type, or a composite type's attribute that uses one
// of them, or an array of one, blocks the drop, unless that domain or
// composite type is itself in drop. Each type is read relative to the schema
// of the object it belongs to, as ResolveTypeRef reads it. Returns an error
// naming the first reference found, or nil if the drop is safe.
func CheckTypeDropRefs(s *Schema, drop []TypeRef) error {
	dropping := func(typ, owner string) (TypeRef, bool) {
		ref, _ := ResolveTypeRef(typ, owner)
		return ref, slices.Contains(drop, ref)
	}
	refErr := func(ref TypeRef, user string) error {
		return fmt.Errorf("cannot drop type %s: %s uses it", qualifiedName(ref.Schema, ref.Name), user)
	}
	for i := range s.Tables {
		t := &s.Tables[i]
		for _, col := range t.Columns {
			if ref, ok := dropping(col.Type, t.Schema); ok {
				return refErr(ref, fmt.Sprintf("column %s.%s", qualifiedName(t.Schema, t.Name), col.Name))
			}
		}
	}
	for i := range s.Views {
		v := &s.Views[i]
		for _, col := range v.Columns {
			if ref, ok := dropping(col.Type, v.Schema); ok {
				return refErr(ref, fmt.Sprintf("column %s.%s of view", qualifiedName(v.Schema, v.Name), col.Name))
			}
		}
	}
	for _, dt := range s.DomainTypes {
		if slices.Contains(drop, TypeRef{Schema: dt.Schema, Name: dt.Name}) {
			continue
		}
		if ref, ok := dropping(dt.BaseType, dt.Schema); ok {
			return refErr(ref, "domain "+qualifiedName(dt.Schema, dt.Name))
		}
	}
	for _, ct := range s.CompositeTypes {
		if slices.Contains(drop, TypeRef{Schema: ct.Schema, Name: ct.Name}) {
			continue
		}
		for _, attr := range ct.Attributes {
			if ref, ok := dropping(attr.Type, ct.Schema); ok {
				return refErr(ref, fmt.Sprintf("attribute %s of composite type %s", attr.Name, qualifiedName(ct.Schema, ct.Name)))
			}
		}
	}
	return nil
}

// ColumnByName returns a pointer to the named column in the table, or nil.
func ColumnByName(table *Table, name string) *Column {
	for i := range table.Columns {
		if table.Columns[i].Name == name {
			return &table.Columns[i]
		}
	}
	return nil
}

// ApplyTableConstraints propagates table-level constraint metadata to columns.
func ApplyTableConstraints(table *Table) {
	for _, c := range table.Constraints {
		ApplyConstraintToColumns(table, c)
	}
}

// ApplyConstraintToColumns updates column flags from a single constraint.
func ApplyConstraintToColumns(table *Table, c Constraint) {
	switch c.Type {
	case PrimaryKey:
		for _, colName := range c.Columns {
			if col := ColumnByName(table, colName); col != nil {
				col.PrimaryKey = true
				col.Nullable = false
			}
		}
	case Unique:
		// Partial UNIQUE indexes (Where != "") do not strictly constrain the
		// column — uniqueness applies only to rows matching the predicate, so
		// the column-level Unique flag (used downstream to drive FindByX and
		// upsert paths) stays unset.
		if len(c.Columns) == 1 && c.Where == "" {
			if col := ColumnByName(table, c.Columns[0]); col != nil {
				col.Unique = true
			}
		}
	case ForeignKey, Check, Index:
		// CHECK and INDEX constraints set no column flags. CanonicalizeConstraints
		// sets FKReference from every FOREIGN KEY at once, since a column may
		// have several.
	}
}

// RecomputeColumnUnique re-derives the column's Unique flag from the
// unconditional, single-column UNIQUE constraints remaining in
// table.Constraints. Callers must remove the dropped constraint from
// table.Constraints before invoking this helper; partial UNIQUE
// constraints (Where != "") are ignored because they cannot keep the
// column unconditionally unique.
func RecomputeColumnUnique(table *Table, colName string) {
	col := ColumnByName(table, colName)
	if col == nil {
		return
	}
	for _, c := range table.Constraints {
		if c.Type != Unique || c.Where != "" || len(c.Columns) != 1 {
			continue
		}
		if c.Columns[0] == colName {
			col.Unique = true
			return
		}
	}
	col.Unique = false
}

// RenameColumn renames the column of table that oldName names to newName,
// matching names as dialect does (SameColumnName). It is an error when no
// column has oldName, or when another column already has newName, as it is in
// the database. MySQL and SQLite let a column be renamed to its own name, in
// any case; PostgreSQL rejects a rename to the name the column already has.
func RenameColumn(table *Table, dialect, oldName, newName string) error {
	i := slices.IndexFunc(table.Columns, func(c Column) bool { return SameColumnName(dialect, c.Name, oldName) })
	if i < 0 {
		return fmt.Errorf("column %s not found in table %s", oldName, table.Name)
	}
	for j, c := range table.Columns {
		if (j != i || dialect == "postgres") && SameColumnName(dialect, c.Name, newName) {
			return fmt.Errorf("column %s already exists in table %s", newName, table.Name)
		}
	}
	table.Columns[i].Name = newName
	return nil
}

func qualifiedName(schema, name string) string {
	if schema == "" {
		return name
	}
	return schema + "." + name
}
