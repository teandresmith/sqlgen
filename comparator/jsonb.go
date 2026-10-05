package comparator

import (
	"github.com/teandresmith/sqlgen/sql"
)

// JSONB filters PostgreSQL JSONB columns with native JSONB operators.
// This comparator should only be used with the PostgreSQL dialect.
type JSONB struct {
	HasKey      *string         `json:"has_key,omitempty"`
	HasAnyKey   []string        `json:"has_any_key,omitempty"`
	HasAllKeys  []string        `json:"has_all_keys,omitempty"`
	Contains    *any            `json:"contains,omitempty"`
	ContainedBy *any            `json:"contained_by,omitempty"`
	PathExists  *string         `json:"path_exists,omitempty"`
	Custom      []sql.Condition `json:"custom,omitempty"`
}

// Parse converts set operator fields into sql.Condition values.
// All conditions use PostgreSQL-specific JSONB operators.
func (c JSONB) Parse(col string, d sql.Dialect) []sql.Condition {
	var conds []sql.Condition
	if c.HasKey != nil {
		conds = append(conds, sql.Condition{Clause: col + " ? $", Value: *c.HasKey, Column: col})
	}
	if len(c.HasAnyKey) > 0 {
		conds = append(conds, sql.Condition{Clause: col + " ?| $", Value: c.HasAnyKey, Column: col})
	}
	if len(c.HasAllKeys) > 0 {
		conds = append(conds, sql.Condition{Clause: col + " ?& $", Value: c.HasAllKeys, Column: col})
	}
	if c.Contains != nil {
		conds = append(conds, sql.Condition{Clause: col + " @> $", Value: *c.Contains, Column: col})
	}
	if c.ContainedBy != nil {
		conds = append(conds, sql.Condition{Clause: col + " <@ $", Value: *c.ContainedBy, Column: col})
	}
	if c.PathExists != nil {
		conds = append(conds, sql.Condition{Clause: col + " @? $", Value: *c.PathExists, Column: col})
	}
	conds = append(conds, c.Custom...)
	return conds
}

// NullableJSONB filters nullable PostgreSQL JSONB columns.
type NullableJSONB struct {
	JSONB
	Null *bool `json:"null,omitempty"`
}

// Parse converts set operator fields into sql.Condition values.
func (c NullableJSONB) Parse(col string, d sql.Dialect) []sql.Condition {
	conds := c.JSONB.Parse(col, d)
	if c.Null != nil {
		conds = append(conds, parseNull(col, c.Null))
	}
	return conds
}
