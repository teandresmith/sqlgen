package comparator

import (
	"github.com/teandresmith/sqlgen/sql"
)

// Opaque filters columns whose Go type is neither text nor a Go numeric, using
// a type parameter for the column's own Go type (PRD §11.2).
//
// The members are the types the built-in dialect tables resolve binary and
// network columns to: `[]byte` (`bytea`, and the MySQL / SQLite `blob` family),
// `net.IP` (`inet`), `net.IPNet` (`cidr`) and `net.HardwareAddr` (`macaddr`).
// Each is equality- and order-comparable in SQL, but none is a Go `string` and
// none satisfies Numeric, so before this family they fell through to String and
// the generated filter compared a binary or network column against a text
// parameter. Because the operand is the column's own Go type, the
// driver encodes it natively: `payload = $1` binds a `[]byte`, not a base64 or
// hex rendering of one.
//
// T is `any`, not `comparable`. Two of the four members are not comparable
// (`[]byte` and `net.IP` are slices, `net.IPNet` contains one), and the
// constraint would buy nothing regardless — no comparator body compares in Go,
// they only emit sql.Condition values whose operands go straight to the driver.
// Enum's `comparable` is likewise nominal; it is kept there because enum Go
// types always satisfy it and the constraint documents intent.
//
// The text operators String carries — Contains, StartsWith, EndsWith, Like,
// NLike — are deliberately absent. `inet` and `macaddr` have no LIKE operator
// at all (PostgreSQL raises SQLSTATE 42883), and on `bytea` / `blob` pattern
// matching runs against raw bytes, which is not what a caller filtering a
// signature or a MAC address means.
type Opaque[T any] struct {
	Eq     *T              `json:"eq,omitempty"`
	Neq    *T              `json:"neq,omitempty"`
	In     []T             `json:"in,omitempty"`
	Nin    []T             `json:"nin,omitempty"`
	Gt     *T              `json:"gt,omitempty"`
	Gte    *T              `json:"gte,omitempty"`
	Lt     *T              `json:"lt,omitempty"`
	Lte    *T              `json:"lte,omitempty"`
	Custom []sql.Condition `json:"custom,omitempty"`
}

// Parse converts set operator fields into sql.Condition values.
func (c Opaque[T]) Parse(col string, d sql.Dialect) []sql.Condition {
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

// NullableOpaque filters nullable opaque columns.
type NullableOpaque[T any] struct {
	Opaque[T]
	Null *bool `json:"null,omitempty"`
}

// Parse converts set operator fields into sql.Condition values.
func (c NullableOpaque[T]) Parse(col string, d sql.Dialect) []sql.Condition {
	conds := c.Opaque.Parse(col, d)
	if c.Null != nil {
		conds = append(conds, parseNull(col, c.Null))
	}
	return conds
}
