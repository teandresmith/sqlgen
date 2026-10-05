package comparator

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/teandresmith/sqlgen/sql"
)

// Slice filters PostgreSQL array columns with native array operators.
// This comparator should only be used with the PostgreSQL dialect.
type Slice[T comparable] struct {
	ContainsAny []T             `json:"contains_any,omitempty"`
	ContainsAll []T             `json:"contains_all,omitempty"`
	ContainedBy []T             `json:"contained_by,omitempty"`
	IsEmpty     *bool           `json:"is_empty,omitempty"`
	Custom      []sql.Condition `json:"custom,omitempty"`
}

// Parse converts set operator fields into sql.Condition values.
// All conditions use PostgreSQL-specific array operators.
// Slice values are encoded as PostgreSQL array text literals so that
// pgx can send them without requiring custom type OID registration.
func (c Slice[T]) Parse(col string, d sql.Dialect) []sql.Condition {
	var conds []sql.Condition
	if len(c.ContainsAny) > 0 {
		conds = append(conds, sql.Condition{Clause: col + " && $", Value: sliceToArrayLiteral(c.ContainsAny), Column: col})
	}
	if len(c.ContainsAll) > 0 {
		conds = append(conds, sql.Condition{Clause: col + " @> $", Value: sliceToArrayLiteral(c.ContainsAll), Column: col})
	}
	if len(c.ContainedBy) > 0 {
		conds = append(conds, sql.Condition{Clause: col + " <@ $", Value: sliceToArrayLiteral(c.ContainedBy), Column: col})
	}
	if c.IsEmpty != nil {
		if *c.IsEmpty {
			conds = append(conds, sql.Condition{Clause: col + " = '{}'", Value: nil, Column: col})
		} else {
			conds = append(conds, sql.Condition{Clause: col + " != '{}'", Value: nil, Column: col})
		}
	}
	conds = append(conds, c.Custom...)
	return conds
}

// sliceToArrayLiteral converts a Go slice to a PostgreSQL array text literal
// (e.g. `{admin,editor}`). Sending the literal as a string is what lets pgx
// carry an ENUM array without registering the type's OID — it is the reason
// this comparator hands the driver a string rather than a typed slice, and
// why it never hit the encode failure Enum's set operators did.
//
// Elements are quoted per PostgreSQL's array-literal grammar, which is not
// cosmetic. An unquoted element containing a comma splits into two
// on the server, so the predicate matches a DIFFERENT set and returns
// silently wrong rows; a brace or double quote fails outright with
// `malformed array literal`. Enum members are identifiers and never need
// quoting, which is why the defect stayed invisible — but `Slice[string]` on
// a `text[]` column carries whatever the caller sends.
func sliceToArrayLiteral[T comparable](vals []T) string {
	var b strings.Builder
	b.WriteByte('{')
	for i, v := range vals {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(quoteArrayElement(fmt.Sprint(v)))
	}
	b.WriteByte('}')
	return b.String()
}

// quoteArrayElement double-quotes one array element when PostgreSQL's grammar
// requires it, escaping the two characters that stay special inside quotes.
//
// Quoting unconditionally would also parse, but this mirrors PostgreSQL's own
// `array_out`: numeric, uuid and enum elements keep their bare spelling, so
// the literal a filter sends reads the same as one the server would print.
func quoteArrayElement(s string) string {
	if !arrayElementNeedsQuote(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		if r == '"' || r == '\\' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

// arrayElementNeedsQuote reports whether an element must be double-quoted.
//
// Each case is a distinct way an unquoted element is misread rather than
// rejected: the empty string vanishes entirely; a case-insensitive `NULL` is
// taken as a NULL element instead of the four-character string; the delimiter
// and the braces terminate the element early; a quote or backslash is
// consumed as syntax; and leading or trailing whitespace is stripped.
func arrayElementNeedsQuote(s string) bool {
	if s == "" || strings.EqualFold(s, "NULL") {
		return true
	}
	return strings.ContainsAny(s, `{},"\`) || strings.ContainsFunc(s, unicode.IsSpace)
}

// NullableSlice filters nullable PostgreSQL array columns.
type NullableSlice[T comparable] struct {
	Slice[T]
	Null *bool `json:"null,omitempty"`
}

// Parse converts set operator fields into sql.Condition values.
func (c NullableSlice[T]) Parse(col string, d sql.Dialect) []sql.Condition {
	conds := c.Slice.Parse(col, d)
	if c.Null != nil {
		conds = append(conds, parseNull(col, c.Null))
	}
	return conds
}
