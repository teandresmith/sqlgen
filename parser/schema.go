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
}

// Column represents a column in a table.
type Column struct {
	Name       string
	Type       string
	Nullable   bool
	PrimaryKey bool
	Unique     bool
	// InlineUnique reports whether the column carries a UNIQUE constraint
	// declared inline at column definition time (e.g. `email text UNIQUE`),
	// as opposed to a table-level UNIQUE constraint that lives in
	// Table.Constraints. The DDL parsers set this alongside Unique at column
	// parse time; RecomputeColumnUnique consults it when re-deriving the
	// effective Unique flag after a table-level UNIQUE is dropped, so an
	// inline declaration still in force at the DB level is not silently
	// stripped from the column.
	InlineUnique     bool
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
	// typed — must exclude it. Column is empty for synthetic references, but
	// that is not a usable test: the MySQL and SQLite parsers also leave it
	// empty when a REFERENCES clause omits the referenced column list.
	Synthetic bool
}

// Constraint represents a table-level constraint.
//
// Method and Where apply only to index-shaped constraints (Index and Unique).
// Method holds the access-method name when the DDL declares one — "btree",
// "gin", "gist", "hash", and so on — and is empty otherwise; consumers that
// need a default treat the empty string as "btree" (PostgreSQL's default).
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
}

// RenameTableReferences repoints every foreign key that references the table
// (schema, oldName) at (schema, newName), so a RENAME TABLE keeps the keys
// that reference the table resolvable.
func RenameTableReferences(tables []Table, schema, oldName, newName string) {
	retargetTableReferences(tables, schema, oldName, schema, newName)
}

// MoveTableReferences repoints every foreign key that references the table
// (oldSchema, name) at (newSchema, name), so an ALTER TABLE … SET SCHEMA keeps
// the keys that reference the table resolvable.
func MoveTableReferences(tables []Table, name, oldSchema, newSchema string) {
	retargetTableReferences(tables, oldSchema, name, newSchema, name)
}

func retargetTableReferences(tables []Table, fromSchema, fromName, toSchema, toName string) {
	for i := range tables {
		t := &tables[i]
		for j := range t.Columns {
			ref := t.Columns[j].FKReference
			if ref != nil && ref.Schema == fromSchema && ref.Table == fromName {
				ref.Schema, ref.Table = toSchema, toName
			}
		}
		for j := range t.Constraints {
			c := &t.Constraints[j]
			if c.Type == ForeignKey && c.ReferenceSchema == fromSchema && c.ReferenceTable == fromName {
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
// conflict target names a column that no longer exists. Call it after
// RenameColumn has renamed the column itself.
func RenameColumnReferences(tables []Table, schema, table, oldName, newName string) {
	for i := range tables {
		t := &tables[i]
		owner := t.Schema == schema && t.Name == table
		for j := range t.Columns {
			ref := t.Columns[j].FKReference
			if ref != nil && ref.Schema == schema && ref.Table == table && ref.Column == oldName {
				ref.Column = newName
			}
		}
		for j := range t.Constraints {
			c := &t.Constraints[j]
			if owner {
				replaceName(c.Columns, oldName, newName)
			}
			if c.Type == ForeignKey && c.ReferenceSchema == schema && c.ReferenceTable == table {
				replaceName(c.ReferenceColumns, oldName, newName)
			}
		}
	}
}

// replaceName replaces every oldName in names with newName, in place.
func replaceName(names []string, oldName, newName string) {
	for i := range names {
		if names[i] == oldName {
			names[i] = newName
		}
	}
}

// UnresolvedForeignKeys returns one error per foreign key whose referenced
// table is not in tables, sorted for deterministic output. Relationship
// detection builds an edge from every foreign key, and an edge whose far end
// was never parsed is dropped without a trace, so the caller reports these
// instead.
func UnresolvedForeignKeys(tables []Table) []error {
	known := make(map[string]bool, len(tables))
	for i := range tables {
		known[qualifiedName(tables[i].Schema, tables[i].Name)] = true
	}
	seen := make(map[string]bool)
	var msgs []string
	add := func(t *Table, cols, refSchema, refTable string) {
		target := qualifiedName(refSchema, refTable)
		if known[target] {
			return
		}
		msg := fmt.Sprintf("foreign key %s(%s) references %s, which no parsed table defines",
			qualifiedName(t.Schema, t.Name), cols, target)
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

// CheckTableDropRefs checks whether any table in the schema has a foreign key
// reference to the table identified by (schema, name). Returns an error
// describing the first dangling reference found, or nil if the drop is safe.
func CheckTableDropRefs(tables []Table, schema, name string) error {
	for i := range tables {
		t := &tables[i]
		if t.Schema == schema && t.Name == name {
			continue // self — will be dropped
		}
		for _, col := range t.Columns {
			if col.FKReference != nil && col.FKReference.Schema == schema && col.FKReference.Table == name {
				return fmt.Errorf(
					"cannot drop table %s: column %s.%s references it via foreign key",
					qualifiedName(schema, name), qualifiedName(t.Schema, t.Name), col.Name,
				)
			}
		}
		for _, c := range t.Constraints {
			if c.Type == ForeignKey && c.ReferenceSchema == schema && c.ReferenceTable == name {
				return fmt.Errorf(
					"cannot drop table %s: table %s has a foreign key constraint referencing it",
					qualifiedName(schema, name), qualifiedName(t.Schema, t.Name),
				)
			}
		}
	}
	return nil
}

// CheckTypeDropRefs checks whether any column in the schema uses the given
// type name. Returns an error if a reference is found, or nil if the drop is safe.
func CheckTypeDropRefs(tables []Table, schema, name string) error {
	qualified := qualifiedName(schema, name)
	for i := range tables {
		t := &tables[i]
		for _, col := range t.Columns {
			if col.Type == name || col.Type == qualified {
				return fmt.Errorf(
					"cannot drop type %s: column %s.%s uses it",
					qualified, qualifiedName(t.Schema, t.Name), col.Name,
				)
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
	case ForeignKey:
		if len(c.Columns) == 1 && len(c.ReferenceColumns) == 1 {
			if col := ColumnByName(table, c.Columns[0]); col != nil && col.FKReference == nil {
				col.FKReference = &FKReference{
					Table:  c.ReferenceTable,
					Schema: c.ReferenceSchema,
					Column: c.ReferenceColumns[0],
				}
			}
		}
	case Check, Index:
		// CHECK and INDEX constraints don't affect column flags.
	}
}

// RecomputeColumnUnique re-derives the column's Unique flag from the
// current table state — the column's InlineUnique flag plus any
// unconditional, single-column UNIQUE constraint remaining in
// table.Constraints. Callers must remove the dropped constraint from
// table.Constraints before invoking this helper; partial UNIQUE
// constraints (Where != "") are ignored because they cannot keep the
// column unconditionally unique.
func RecomputeColumnUnique(table *Table, colName string) {
	col := ColumnByName(table, colName)
	if col == nil {
		return
	}
	if col.InlineUnique {
		col.Unique = true
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

// RenameColumn renames a column in the table from oldName to newName.
func RenameColumn(table *Table, oldName, newName string) error {
	for i := range table.Columns {
		if table.Columns[i].Name == oldName {
			table.Columns[i].Name = newName
			return nil
		}
	}
	return fmt.Errorf("column %s not found in table %s", oldName, table.Name)
}

func qualifiedName(schema, name string) string {
	if schema == "" {
		return name
	}
	return schema + "." + name
}
