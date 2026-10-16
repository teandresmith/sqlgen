package parser

import "fmt"

// DependencyKind is the kind of object a view depends on.
type DependencyKind string

// The kinds of object a view depends on. A column or constraint belongs to
// a table, and an attribute to a composite type.
const (
	TableDependency      DependencyKind = "table"
	ColumnDependency     DependencyKind = "column"
	ConstraintDependency DependencyKind = "constraint"
	TypeDependency       DependencyKind = "type"
	AttributeDependency  DependencyKind = "attribute"
)

// ViewDependency is an object a database view depends on, as the database
// records it.
type ViewDependency struct {
	// ViewSchema and View name the view.
	ViewSchema, View string
	Object           DependencyObject
}

// DependencyObject names an object a view depends on: a table or type by
// Schema and Name, and a column, constraint or attribute of it by Part.
type DependencyObject struct {
	Kind         DependencyKind
	Schema, Name string
	Part         string
}

// onType reports whether k is a type's or one of its attributes'.
func (k DependencyKind) onType() bool {
	return k == TypeDependency || k == AttributeDependency
}

// covers reports whether a dependency on dep is one on o: the same object,
// or a part of o when o is a whole table or type.
func (o DependencyObject) covers(dep DependencyObject) bool {
	if o.Schema != dep.Schema || o.Name != dep.Name || o.Kind.onType() != dep.Kind.onType() {
		return false
	}
	return o.Part == "" && (o.Kind == TableDependency || o.Kind == TypeDependency) || o == dep
}

func (o DependencyObject) String() string {
	owner := qualifiedName(o.Schema, o.Name)
	switch o.Kind {
	case ColumnDependency:
		return fmt.Sprintf("column %s of table %s", o.Part, owner)
	case ConstraintDependency:
		return fmt.Sprintf("constraint %s on table %s", o.Part, owner)
	case AttributeDependency:
		return fmt.Sprintf("attribute %s of type %s", o.Part, owner)
	default:
		return string(o.Kind) + " " + owner
	}
}

// CheckViewDependents returns an error naming the first view in
// s.ViewDependencies that depends on obj, or on a part of it when obj is a
// whole table or type, as PostgreSQL refuses to drop, or change the type
// of, an object a view depends on. action is what the statement does to
// obj: "drop" or "change the type of". The database's view stands under
// source: both, so the view is not dropped along with it, even under
// CASCADE (PRD §6.1).
func CheckViewDependents(s *Schema, action string, obj DependencyObject) error {
	for _, dep := range s.ViewDependencies {
		if obj.covers(dep.Object) {
			return fmt.Errorf("cannot %s %s: view %s depends on it; under source: both the database's view stands, so change it in the database first",
				action, obj, qualifiedName(dep.ViewSchema, dep.View))
		}
	}
	return nil
}

// RenameDependencies repoints every dependency in s.ViewDependencies on
// from at to, as PostgreSQL records a dependency by the object's OID, so it
// follows a rename or SET SCHEMA. A from that names a whole table or type
// repoints the dependencies on its parts too.
func RenameDependencies(s *Schema, from, to DependencyObject) {
	for i := range s.ViewDependencies {
		dep := &s.ViewDependencies[i].Object
		if !from.covers(*dep) {
			continue
		}
		dep.Schema, dep.Name = to.Schema, to.Name
		if from.Part != "" {
			dep.Part = to.Part
		}
	}
}
