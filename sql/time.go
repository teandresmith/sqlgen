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
// would each compare against a different spelling of the same instant. The
// other dialects bind the value unchanged.
func bindArgs(d Dialect, args []any) []any {
	if d.Name() != "sqlite" {
		return args
	}
	var out []any
	for i, arg := range args {
		text, ok := sqliteTime(arg)
		if !ok {
			continue
		}
		if out == nil {
			out = slices.Clone(args)
		}
		out[i] = text
	}
	if out == nil {
		return args
	}
	return out
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
