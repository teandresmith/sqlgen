package comparator

import (
	"github.com/teandresmith/sqlgen/sql"
)

// String filters string columns with text-specific operators.
type String struct {
	Eq         *string         `json:"eq,omitempty"`
	Neq        *string         `json:"neq,omitempty"`
	In         []string        `json:"in,omitempty"`
	Nin        []string        `json:"nin,omitempty"`
	Gt         *string         `json:"gt,omitempty"`
	Gte        *string         `json:"gte,omitempty"`
	Lt         *string         `json:"lt,omitempty"`
	Lte        *string         `json:"lte,omitempty"`
	Contains   *string         `json:"contains,omitempty"`
	StartsWith *string         `json:"starts_with,omitempty"`
	EndsWith   *string         `json:"ends_with,omitempty"`
	Like       *string         `json:"like,omitempty"`
	NLike      *string         `json:"n_like,omitempty"`
	Custom     []sql.Condition `json:"custom,omitempty"`
}

// Parse converts set operator fields into sql.Condition values.
func (c String) Parse(col string, d sql.Dialect) []sql.Condition {
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
	if c.Contains != nil {
		conds = append(conds, sql.Where(col).Like("%"+*c.Contains+"%"))
	}
	if c.StartsWith != nil {
		conds = append(conds, sql.Where(col).Like(*c.StartsWith+"%"))
	}
	if c.EndsWith != nil {
		conds = append(conds, sql.Where(col).Like("%"+*c.EndsWith))
	}
	if c.Like != nil {
		conds = append(conds, sql.Where(col).Like(*c.Like))
	}
	if c.NLike != nil {
		conds = append(conds, sql.Where(col).NLike(*c.NLike))
	}
	conds = append(conds, c.Custom...)
	return conds
}

// NullableString filters nullable string columns.
type NullableString struct {
	String
	Null *bool `json:"null,omitempty"`
}

// Parse converts set operator fields into sql.Condition values.
func (c NullableString) Parse(col string, d sql.Dialect) []sql.Condition {
	conds := c.String.Parse(col, d)
	if c.Null != nil {
		conds = append(conds, parseNull(col, c.Null))
	}
	return conds
}
