package comparator

import (
	"github.com/teandresmith/sqlgen/sql"
)

// ID filters string-based ID columns (primary keys and foreign keys).
type ID struct {
	Eq     *string         `json:"eq,omitempty"`
	Neq    *string         `json:"neq,omitempty"`
	In     []string        `json:"in,omitempty"`
	Nin    []string        `json:"nin,omitempty"`
	Gt     *string         `json:"gt,omitempty"`
	Gte    *string         `json:"gte,omitempty"`
	Lt     *string         `json:"lt,omitempty"`
	Lte    *string         `json:"lte,omitempty"`
	Custom []sql.Condition `json:"custom,omitempty"`
}

// Parse converts set operator fields into sql.Condition values.
func (c ID) Parse(col string, d sql.Dialect) []sql.Condition {
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
	conds = append(conds, c.Custom...)
	return conds
}

// NullableID filters nullable string-based ID columns.
type NullableID struct {
	ID
	Null *bool `json:"null,omitempty"`
}

// Parse converts set operator fields into sql.Condition values.
func (c NullableID) Parse(col string, d sql.Dialect) []sql.Condition {
	conds := c.ID.Parse(col, d)
	if c.Null != nil {
		conds = append(conds, parseNull(col, c.Null))
	}
	return conds
}
