package parser

// DetectSoftDelete checks the table's columns against the given column names in
// priority order and returns the first name the table has, or "" if none
// matches. It matches by name only: the strategy is the configured type, and
// config validation checks that the column's SQL type is compatible with it.
func DetectSoftDelete(table *Table, columns []string) string {
	for _, name := range columns {
		if hasColumn(table, name) {
			return name
		}
	}
	return ""
}

// DetectUpdateColumns finds columns in the table that match the given column
// names. Returns matching column names in the order they appear in the names list.
func DetectUpdateColumns(table *Table, names []string) []string {
	var matched []string
	for _, name := range names {
		if hasColumn(table, name) {
			matched = append(matched, name)
		}
	}
	return matched
}

// hasColumn reports whether the table has a column with the given name.
func hasColumn(table *Table, name string) bool {
	for _, c := range table.Columns {
		if c.Name == name {
			return true
		}
	}
	return false
}
