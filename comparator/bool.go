package comparator

import (
	"github.com/teandresmith/sqlgen/sql"
)

// Bool filters boolean columns.
type Bool struct {
	Eq     *bool           `json:"eq,omitempty"`
	Neq    *bool           `json:"neq,omitempty"`
	Custom []sql.Condition `json:"custom,omitempty"`
}

// Parse converts set operator fields into sql.Condition values.
func (c Bool) Parse(col string, d sql.Dialect) []sql.Condition {
	var conds []sql.Condition
	if c.Eq != nil {
		conds = append(conds, sql.Where(col).Eq(*c.Eq))
	}
	if c.Neq != nil {
		conds = append(conds, sql.Where(col).Neq(*c.Neq))
	}
	conds = append(conds, c.Custom...)
	return conds
}

// NullableBool filters nullable boolean columns.
type NullableBool struct {
	Bool
	Null *bool `json:"null,omitempty"`
}

// Parse converts set operator fields into sql.Condition values.
func (c NullableBool) Parse(col string, d sql.Dialect) []sql.Condition {
	conds := c.Bool.Parse(col, d)
	if c.Null != nil {
		conds = append(conds, parseNull(col, c.Null))
	}
	return conds
}
