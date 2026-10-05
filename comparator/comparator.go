// Package comparator provides type-safe filter operators that produce
// sql.Condition values for use in generated SQLGen query builders.
//
// Every family carries a Custom []sql.Condition escape hatch whose conditions
// are appended to the ones its operator fields produce. A filter's conditions
// are joined with AND, so a custom condition must render as a single AND-able
// term or it pulls its neighbours into its own top-level operator. Build one
// with [sql.Raw], which marks it for parenthesisation; a hand-built
// sql.Condition literal carrying a compound predicate must set sql.Condition.Raw
// itself.
package comparator

import (
	"github.com/teandresmith/sqlgen/sql"
)

// Numeric constrains type parameters to Go numeric types.
type Numeric interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 |
		~float32 | ~float64
}

// Range represents start and end bounds for BETWEEN conditions.
type Range[T any] struct {
	Start T `json:"start"`
	End   T `json:"end"`
}

// parseIn produces a dialect-aware IN condition. Dialects with array parameter
// support (pgx) use = ANY($) with a single array arg; others use IN $ with
// expanded args.
func parseIn[T any](col string, d sql.Dialect, vals []T) sql.Condition {
	if d.SupportsArrayParams() {
		return sql.Condition{Clause: col + " = ANY($)", Value: vals, Column: col}
	}
	return parseInExpanded(col, vals)
}

// parseInExpanded is parseIn's non-array form: one placeholder per value.
// Split out so a comparator whose element type the array path cannot encode
// can ask for it on every dialect (see comparator.Enum).
func parseInExpanded[T any](col string, vals []T) sql.Condition {
	args := make([]any, len(vals))
	for i, v := range vals {
		args[i] = v
	}
	return sql.Condition{Clause: col + " IN $", Value: args, Column: col}
}

// parseNin produces a dialect-aware NOT IN condition. Dialects with array
// parameter support (pgx) use != ALL($) with a single array arg; others use
// NOT IN $ with expanded args.
func parseNin[T any](col string, d sql.Dialect, vals []T) sql.Condition {
	if d.SupportsArrayParams() {
		return sql.Condition{Clause: col + " != ALL($)", Value: vals, Column: col}
	}
	return parseNinExpanded(col, vals)
}

// parseNinExpanded is parseNin's non-array form — see parseInExpanded.
func parseNinExpanded[T any](col string, vals []T) sql.Condition {
	args := make([]any, len(vals))
	for i, v := range vals {
		args[i] = v
	}
	return sql.Condition{Clause: col + " NOT IN $", Value: args, Column: col}
}

// parseNull produces an IS NULL or IS NOT NULL condition based on the value.
func parseNull(col string, null *bool) sql.Condition {
	if *null {
		return sql.Where(col).IsNull()
	}
	return sql.Where(col).IsNotNull()
}

// parseBetween produces a BETWEEN $ AND $ condition from a Range.
func parseBetween[T any](col string, r Range[T]) sql.Condition {
	return sql.Condition{
		Clause: col + " BETWEEN $ AND $",
		Value:  sql.Range{Start: r.Start, End: r.End},
		Column: col,
	}
}

// parseNBetween produces a NOT BETWEEN $ AND $ condition from a Range.
func parseNBetween[T any](col string, r Range[T]) sql.Condition {
	return sql.Condition{
		Clause: col + " NOT BETWEEN $ AND $",
		Value:  sql.Range{Start: r.Start, End: r.End},
		Column: col,
	}
}
