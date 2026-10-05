package comparator

import (
	"github.com/teandresmith/sqlgen/sql"
)

// Number filters numeric columns using a type parameter constrained to Numeric.
type Number[T Numeric] struct {
	Eq       *T              `json:"eq,omitempty"`
	Neq      *T              `json:"neq,omitempty"`
	In       []T             `json:"in,omitempty"`
	Nin      []T             `json:"nin,omitempty"`
	Gt       *T              `json:"gt,omitempty"`
	Gte      *T              `json:"gte,omitempty"`
	Lt       *T              `json:"lt,omitempty"`
	Lte      *T              `json:"lte,omitempty"`
	Between  *Range[T]       `json:"between,omitempty"`
	NBetween *Range[T]       `json:"n_between,omitempty"`
	Custom   []sql.Condition `json:"custom,omitempty"`
}

// Parse converts set operator fields into sql.Condition values.
func (c Number[T]) Parse(col string, d sql.Dialect) []sql.Condition {
	var conds []sql.Condition
	if c.Eq != nil {
		conds = append(conds, sql.Where(col).Eq(*c.Eq))
	}
	if c.Neq != nil {
		conds = append(conds, sql.Where(col).Neq(*c.Neq))
	}
	if len(c.In) > 0 {
		conds = append(conds, parseIn(col, d, c.In))
	}
	if len(c.Nin) > 0 {
		conds = append(conds, parseNin(col, d, c.Nin))
	}
	if c.Gt != nil {
		conds = append(conds, sql.Where(col).Gt(*c.Gt))
	}
	if c.Gte != nil {
		conds = append(conds, sql.Where(col).Gte(*c.Gte))
	}
	if c.Lt != nil {
		conds = append(conds, sql.Where(col).Lt(*c.Lt))
	}
	if c.Lte != nil {
		conds = append(conds, sql.Where(col).Lte(*c.Lte))
	}
	if c.Between != nil {
		conds = append(conds, parseBetween(col, *c.Between))
	}
	if c.NBetween != nil {
		conds = append(conds, parseNBetween(col, *c.NBetween))
	}
	conds = append(conds, c.Custom...)
	return conds
}

// NullableNumber filters nullable numeric columns.
type NullableNumber[T Numeric] struct {
	Number[T]
	Null *bool `json:"null,omitempty"`
}

// Parse converts set operator fields into sql.Condition values.
func (c NullableNumber[T]) Parse(col string, d sql.Dialect) []sql.Condition {
	conds := c.Number.Parse(col, d)
	if c.Null != nil {
		conds = append(conds, parseNull(col, c.Null))
	}
	return conds
}
