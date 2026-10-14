package sql

import (
	stdsql "database/sql"
	"slices"
	"time"

	"github.com/teandresmith/sqlgen/types"
)

// sqliteTimeLayout is the text SQLite's datetime() and CURRENT_TIMESTAMP
// write, followed by the fractional seconds a Go time carries.
const sqliteTimeLayout = "2006-01-02 15:04:05.999999999"

// bindArgs returns args as d binds them.
//
// SQLite has no date/time storage class and compares date/time values as
// text, so on SQLite a time value binds as the text datetime() writes, in UTC.
// The driver's own time.Time format and types.DateTime's RFC 3339 output match
// neither that text nor each other, so a write, a filter and a cursor keyset
// would each compare against a different spelling of the same instant.
//
// On MySQL a types.DateTime binds as the time.Time it wraps. MySQL rejects the
// "Z" that ends the RFC 3339 text of a UTC time and moves any other offset to
// the session time zone, while the driver writes a time.Time as wall time in
// its loc and parses a DATETIME back in that loc. Binding the time.Time gives a
// types.DateTime column the round trip of a time.Time column.
//
// PostgreSQL binds the value unchanged: its drivers send a time.Time with its
// offset, and a types.DateTime's RFC 3339 text carries one too.
func bindArgs(d Dialect, args []any) []any {
	var bind func(any) (any, bool)
	switch d.Name() {
	case "sqlite":
		bind = sqliteTime
	case "mysql":
		bind = mysqlTime
	default:
		return args
	}
	var out []any
	for i, arg := range args {
		v, ok := bind(arg)
		if !ok {
			continue
		}
		if out == nil {
			out = slices.Clone(args)
		}
		out[i] = v
	}
	if out == nil {
		return args
	}
	return out
}

// mysqlTime returns a types.DateTime value as the time.Time it wraps, or nil
// for a null. It reports false for any other value, a time.Time included,
// which the driver binds itself.
func mysqlTime(v any) (any, bool) {
	switch v := v.(type) {
	case types.DateTime:
		return v.Time, true
	case *types.DateTime:
		if v == nil {
			return nil, true
		}
		return v.Time, true
	case types.NullDateTime:
		if !v.Valid {
			return nil, true
		}
		return v.Time, true
	default:
		return nil, false
	}
}

// sqliteTime returns v as SQLite time text, or nil for a null time. It
// reports false for a value that is not a time.
func sqliteTime(v any) (any, bool) {
	var t time.Time
	switch v := v.(type) {
	case time.Time:
		t = v
	case *time.Time:
		if v == nil {
			return nil, true
		}
		t = *v
	case stdsql.NullTime:
		if !v.Valid {
			return nil, true
		}
		t = v.Time
	case types.DateTime:
		t = v.Time
	case *types.DateTime:
		if v == nil {
			return nil, true
		}
		t = v.Time
	case types.NullDateTime:
		if !v.Valid {
			return nil, true
		}
		t = v.Time
	default:
		return nil, false
	}
	return t.UTC().Format(sqliteTimeLayout), true
}
