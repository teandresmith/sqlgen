package comparator

import (
	"github.com/teandresmith/sqlgen/sql"
)

// JSON filters JSON columns with dialect-specific operators.
// PostgreSQL casts the column to jsonb and uses the @> and ? operators;
// MySQL uses the JSON_CONTAINS and JSON_CONTAINS_PATH functions.
//
// Those are the only two dialects the family covers. Parse contributes no
// conditions on any other, SQLite included: SQLite has a faithful HasKey
// (json_type(col, path) IS NOT NULL) but no containment operator at all, and
// every emulation of Contains that SQLite can express is position-sensitive
// where @> and JSON_CONTAINS are element-wise, so a filter would silently
// return a wrong subset. The generator does not emit a JSON filter field for
// a SQLite column, so no generated surface can reach that silence — a
// hand-written filter still can (PRD §11.2).
//
// The PostgreSQL cast is load-bearing, not cosmetic. Containment (@>) and
// existence (?) are defined for jsonb alone — PostgreSQL gives the json type
// only ->, ->>, #> and #>> — so `settings @> $1` on a json column fails with
// "operator does not exist: json @> unknown" (SQLSTATE 42883). The cast is
// also free where the column is already jsonb: the planner elides a same-type
// cast, so a GIN index still matches. That is what lets one arm serve both
// column types, which it must, since Parse sees a column name and a dialect
// but never the column's SQL type.
//
// Two consequences reach the caller. A json column holding a \u0000 escape
// cannot be cast at all (jsonb cannot represent it), so a filter scanning such
// a row fails at request time — unavoidable while keeping the operators, since
// containment on json has no json-native form. And HasKey's operand is
// dialect-specific: PostgreSQL's ? takes a bare key name ("region") while
// MySQL's JSON_CONTAINS_PATH takes a JSON path ("$.region").
type JSON struct {
	Contains *any            `json:"contains,omitempty"`
	HasKey   *string         `json:"has_key,omitempty"`
	Custom   []sql.Condition `json:"custom,omitempty"`
}

// Parse converts set operator fields into sql.Condition values.
// The dialect determines which SQL syntax is produced.
func (c JSON) Parse(col string, d sql.Dialect) []sql.Condition {
	var conds []sql.Condition
	if c.Contains != nil {
		switch d.Name() {
		case "postgres":
			conds = append(conds, sql.Condition{Clause: col + "::jsonb @> $", Value: *c.Contains, Column: col})
		case "mysql":
			conds = append(conds, sql.Condition{Clause: "JSON_CONTAINS(" + col + ", $)", Value: *c.Contains, Column: col})
		}
	}
	if c.HasKey != nil {
		switch d.Name() {
		case "postgres":
			conds = append(conds, sql.Condition{Clause: col + "::jsonb ? $", Value: *c.HasKey, Column: col})
		case "mysql":
			conds = append(conds, sql.Condition{Clause: "JSON_CONTAINS_PATH(" + col + ", 'one', $)", Value: *c.HasKey, Column: col})
		}
	}
	conds = append(conds, c.Custom...)
	return conds
}

// NullableJSON filters nullable JSON columns.
type NullableJSON struct {
	JSON
	Null *bool `json:"null,omitempty"`
}

// Parse converts set operator fields into sql.Condition values.
// Null tests the column itself and so takes no jsonb cast on PostgreSQL —
// only the document operators need one.
func (c NullableJSON) Parse(col string, d sql.Dialect) []sql.Condition {
	conds := c.JSON.Parse(col, d)
	if c.Null != nil {
		conds = append(conds, parseNull(col, c.Null))
	}
	return conds
}
