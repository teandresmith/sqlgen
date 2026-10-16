package parser

import "strings"

// SameTableName reports whether a and b name the same table in dialect
// ("postgres", "mysql" or "sqlite"), compared as the database compares them.
// PostgreSQL compares exactly, because the parser has already folded an
// unquoted name. MySQL compares exactly, as it does with the
// lower_case_table_names=0 default of a case-sensitive file system. SQLite
// ignores ASCII case only, so Users and users are one table while "É" and "é"
// are two.
func SameTableName(dialect, a, b string) bool {
	if dialect == "sqlite" {
		return FoldASCII(a) == FoldASCII(b)
	}
	return a == b
}

// SameColumnName reports whether a and b name the same column in dialect,
// compared as the database compares them. PostgreSQL compares exactly. MySQL
// ignores case, Unicode case included, so "É" and "é" are one column. SQLite
// ignores ASCII case only.
func SameColumnName(dialect, a, b string) bool {
	switch dialect {
	case "mysql":
		return strings.EqualFold(a, b)
	case "sqlite":
		return FoldASCII(a) == FoldASCII(b)
	default:
		return a == b
	}
}

// FoldASCII lowers the ASCII letters in an identifier, as SQLite does when it
// matches names: Users and users are one table, while Äb and äb are two.
func FoldASCII(name string) string {
	b := []byte(name)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// tableKey returns the key a table name is looked up under in dialect: the
// name, with its ASCII case folded on SQLite.
func tableKey(dialect, name string) string {
	if dialect == "sqlite" {
		return FoldASCII(name)
	}
	return name
}

// ResolveReferenceNames rewrites every foreign key's referenced table and
// columns to the spelling the referenced table declares, wherever the dialect
// matches the written name to it (SameTableName, SameColumnName). SQLite and
// MySQL keep a reference as written, so `REFERENCES users(id)` against
// `CREATE TABLE Users` names a table no exact comparison finds, and
// relationship detection would drop the edge. It also gives both sources one
// spelling (PRD §6.1): the MySQL catalog stores a referenced column as the
// table declares it, the SQLite catalog as written. A reference to a table the
// schema does not hold is left alone. Run it on the finished schema, after the
// last statement and before relationship detection.
func ResolveReferenceNames(schema *Schema, dialect string) {
	tables := schema.Tables
	for i := range tables {
		t := &tables[i]
		for j := range t.Columns {
			ref := t.Columns[j].FKReference
			if ref == nil || ref.Synthetic {
				continue
			}
			parent := referencedTable(tables, dialect, ref.Schema, ref.Table)
			if parent == nil {
				continue
			}
			ref.Table = parent.Name
			if ref.Column != "" {
				ref.Column = declaredColumnName(parent, dialect, ref.Column)
			}
		}
		for j := range t.Constraints {
			c := &t.Constraints[j]
			if c.Type != ForeignKey {
				continue
			}
			parent := referencedTable(tables, dialect, c.ReferenceSchema, c.ReferenceTable)
			if parent == nil {
				continue
			}
			c.ReferenceTable = parent.Name
			for k, name := range c.ReferenceColumns {
				c.ReferenceColumns[k] = declaredColumnName(parent, dialect, name)
			}
		}
	}
}

// referencedTable returns the table in tables that (schema, name) names in
// dialect, or nil. An exact match wins.
func referencedTable(tables []Table, dialect, schema, name string) *Table {
	var match *Table
	for i := range tables {
		t := &tables[i]
		if t.Schema != schema {
			continue
		}
		if t.Name == name {
			return t
		}
		if match == nil && SameTableName(dialect, t.Name, name) {
			match = t
		}
	}
	return match
}

// declaredColumnName returns name spelled as table declares the column it
// names in dialect, or name itself when the table has no such column. An exact
// match wins.
func declaredColumnName(table *Table, dialect, name string) string {
	if ColumnByName(table, name) != nil {
		return name
	}
	for _, col := range table.Columns {
		if SameColumnName(dialect, col.Name, name) {
			return col.Name
		}
	}
	return name
}
