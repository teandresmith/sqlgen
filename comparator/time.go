package comparator

import (
	"time"

	"github.com/teandresmith/sqlgen/sql"
)

// Time filters time.Time columns with the same operators as Number.
type Time struct {
	Eq       *time.Time        `json:"eq,omitempty"`
	Neq      *time.Time        `json:"neq,omitempty"`
	In       []time.Time       `json:"in,omitempty"`
	Nin      []time.Time       `json:"nin,omitempty"`
	Gt       *time.Time        `json:"gt,omitempty"`
	Gte      *time.Time        `json:"gte,omitempty"`
	Lt       *time.Time        `json:"lt,omitempty"`
	Lte      *time.Time        `json:"lte,omitempty"`
	Between  *Range[time.Time] `json:"between,omitempty"`
	NBetween *Range[time.Time] `json:"n_between,omitempty"`
	Custom   []sql.Condition   `json:"custom,omitempty"`
}

// Parse converts set operator fields into sql.Condition values.
func (c Time) Parse(col string, d sql.Dialect) []sql.Condition {
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

// NullableTime filters nullable time.Time columns.
type NullableTime struct {
	Time
	Null *bool `json:"null,omitempty"`
}

// Parse converts set operator fields into sql.Condition values.
func (c NullableTime) Parse(col string, d sql.Dialect) []sql.Condition {
	conds := c.Time.Parse(col, d)
	if c.Null != nil {
		conds = append(conds, parseNull(col, c.Null))
	}
	return conds
}
