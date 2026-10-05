package gen

import (
	"slices"
	"strings"
)

// BuildSorterContext builds the SorterContext from table contexts.
// Each table gets a sorter struct with methods for each column.
func BuildSorterContext(pkg string, tables []TableContext) SorterContext {
	sorterTables := make([]SorterTableContext, 0, len(tables))
	for _, tc := range tables {
		cols := make([]SorterColumnContext, 0, len(tc.Columns))
		for _, col := range tc.Columns {
			cols = append(cols, SorterColumnContext{
				ColumnName: col.Name,
				MethodName: col.FieldName,
			})
		}
		slices.SortFunc(cols, func(a, b SorterColumnContext) int {
			return strings.Compare(a.ColumnName, b.ColumnName)
		})
		sorterTables = append(sorterTables, SorterTableContext{
			StructName:     tc.StructName,
			SorterTypeName: tc.StructName + "Sorter",
			Columns:        cols,
		})
	}
	slices.SortFunc(sorterTables, func(a, b SorterTableContext) int {
		return strings.Compare(a.StructName, b.StructName)
	})

	return SorterContext{
		Package: pkg,
		Imports: []string{"github.com/teandresmith/sqlgen/sql"},
		Tables:  sorterTables,
	}
}
