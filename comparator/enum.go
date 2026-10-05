package comparator

import (
	"github.com/teandresmith/sqlgen/sql"
)

// Enum filters enum columns using a type parameter for the enum's Go type.
type Enum[T comparable] struct {
	Eq     *T              `json:"eq,omitempty"`
	Neq    *T              `json:"neq,omitempty"`
	In     []T             `json:"in,omitempty"`
	Nin    []T             `json:"nin,omitempty"`
	Custom []sql.Condition `json:"custom,omitempty"`
}

// Parse converts set operator fields into sql.Condition values.
func (c Enum[T]) Parse(col string, d sql.Dialect) []sql.Condition {
	var conds []sql.Condition
	if c.Eq != nil {
		conds = append(conds, sql.Where(col).Eq(*c.Eq))
	}
	if c.Neq != nil {
		conds = append(conds, sql.Where(col).Neq(*c.Neq))
	}
	// The set operators deliberately take the EXPANDED form on every
	// dialect, rather than the array parameter pgx normally gets. A
	// generated enum is a named string type (`type OrderStatus string`), and
	// pgx has no encode plan for a slice of one against the column's
	// enum-array OID — `= ANY($1)` failed the query outright:
	//
	//	unable to encode []OrderStatus{"spv"} into text format
	//	for unknown type (OID 16494): cannot find encode plan
	//
	// The scalar operators were unaffected, which is why `Eq` worked while
	// `In` did not: a single named string goes through the driver's
	// string-kind conversion, and `IN ($, $, …)` asks for nothing more than
	// that. Both facts are MEASURED against pgx v5 and PostgreSQL 16.
	//
	// Expanding costs one placeholder per value where the other comparators
	// spend one per set. That is the right trade here and nowhere else: an
	// enum column's value domain is its declared members, so the list is
	// bounded by the type rather than by the caller — unlike `ID.In`, which
	// is exactly where the array parameter earns its keep.
	if len(c.In) > 0 {
		conds = append(conds, parseInExpanded(col, c.In))
	}
	if len(c.Nin) > 0 {
		conds = append(conds, parseNinExpanded(col, c.Nin))
	}
	conds = append(conds, c.Custom...)
	return conds
}

// NullableEnum filters nullable enum columns.
type NullableEnum[T comparable] struct {
	Enum[T]
	Null *bool `json:"null,omitempty"`
}

// Parse converts set operator fields into sql.Condition values.
func (c NullableEnum[T]) Parse(col string, d sql.Dialect) []sql.Condition {
	conds := c.Enum.Parse(col, d)
	if c.Null != nil {
		conds = append(conds, parseNull(col, c.Null))
	}
	return conds
}
