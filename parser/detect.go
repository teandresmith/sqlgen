package parser

import (
	"fmt"
	"strings"
)

// DetectSoftDelete checks the table's columns against the given column names in
// priority order. The first matching column determines the soft delete column.
// The strategy is inferred from the column's SQL type: "timestamp" for
// timestamp/timestamptz/datetime, "bool" for bool/boolean, "integer" for
// int/integer/smallint/tinyint. Returns empty strings if no column matches.
// Returns an error if a matching column has an unsupported type.
func DetectSoftDelete(table *Table, columns []string) (column string, strategy string, err error) {
	for _, name := range columns {
		col, found := findColumn(table, name)
		if !found {
			continue
		}
		s, ok := softDeleteStrategy(col.Type)
		if !ok {
			return "", "", fmt.Errorf("soft delete column %q in table %q has unsupported type %q", name, table.Name, col.Type)
		}
		return name, s, nil
	}
	return "", "", nil
}

// DetectUpdateColumns finds columns in the table that match the given column
// names. Returns matching column names in the order they appear in the names list.
func DetectUpdateColumns(table *Table, names []string) []string {
	var matched []string
	for _, name := range names {
		if _, found := findColumn(table, name); found {
			matched = append(matched, name)
		}
	}
	return matched
}

// findColumn returns the column with the given name from the table, if it exists.
func findColumn(table *Table, name string) (Column, bool) {
	for _, c := range table.Columns {
		if c.Name == name {
			return c, true
		}
	}
	return Column{}, false
}

// softDeleteStrategy maps a SQL column type to a soft delete strategy.
// Returns the strategy name and true if the type is compatible, or empty string
// and false if the type is not a valid soft delete type.
func softDeleteStrategy(sqlType string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(sqlType))
	base, _, _ := strings.Cut(normalized, " ")

	switch base {
	case "timestamp", "timestamptz", "datetime":
		return "timestamp", true
	case "bool", "boolean":
		return "bool", true
	case "int", "integer", "smallint", "tinyint":
		return "integer", true
	default:
		return "", false
	}
}
