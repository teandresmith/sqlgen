// Package customtypes hosts hand-written Go types referenced by the postgres
// example's sqlgen.yml `overrides.types` configuration. They exercise the
// user-declared Null-wrapper surface — types whose nullable variant uses a
// non-default valid_field and a non-default underlying_field, instead of the
// canonical `Valid` + struct-name convention used by uuid.NullUUID,
// decimal.NullDecimal, and database/sql.NullX.
package customtypes

import (
	"database/sql/driver"
	"fmt"
)

// OptionalString is a custom Null-wrapper around a string scalar. It scans
// from a NULL-aware text column and serializes back via driver.Value. The
// wrapper exposes Set (rather than Valid) and Val (rather than the embedded
// String/UUID/Decimal field on database/sql.NullX or the integration
// wrappers) so the postgres example covers the path where a user
// declares both `valid_field: Set` and `underlying_field: Val` in
// sqlgen.yml's overrides.types.<sql_type>.nullable block. (Val is named
// distinctly from `Value` because driver.Valuer requires a Value() method,
// and Go forbids a field and method sharing a name on the same struct.)
type OptionalString struct {
	Val string
	Set bool
}

// Scan implements sql.Scanner. nil source clears the value; any string-y
// source populates Val and marks Set true.
func (o *OptionalString) Scan(src any) error {
	if src == nil {
		o.Val = ""
		o.Set = false
		return nil
	}
	switch v := src.(type) {
	case string:
		o.Val = v
	case []byte:
		o.Val = string(v)
	default:
		return fmt.Errorf("OptionalString.Scan: unsupported source type %T", src)
	}
	o.Set = true
	return nil
}

// Value implements driver.Valuer. An unset OptionalString round-trips as a
// SQL NULL; a set value writes through as a string.
func (o OptionalString) Value() (driver.Value, error) {
	if !o.Set {
		return nil, nil
	}
	return o.Val, nil
}
