package database

import (
	stdsql "database/sql"
	"fmt"
)

// NullScan wraps dst as a scan destination that reads SQL NULL as T's zero
// value instead of failing.
//
// It exists for the O2O LEFT JOIN read path. That query selects the target's
// columns alongside the parent's and detects a miss by testing the target's PK
// against its zero value — but on a miss the driver hands back NULL for every
// one of the target's columns, and a NOT NULL column resolves to a Go type that
// refuses it ("converting NULL to int64 is unsupported" on database/sql,
// "cannot scan NULL into *[16]byte" on pgx). The scan failed before the
// zero-value test could run, so an O2O edge errored for any parent lacking a
// matching row. Wrapping the destination defers that decision to the miss
// detection, which is where it belongs.
//
// The conversion for a non-NULL value is the standard library's, so a T whose
// pointer implements [database/sql.Scanner] — a UUID or decimal type, say — is
// still scanned through it.
func NullScan[T any](dst *T) any {
	return nullScanner[T]{dst: dst}
}

// nullScanner is the [stdsql.Scanner] NullScan hands the driver.
type nullScanner[T any] struct {
	dst *T
}

// Scan implements [stdsql.Scanner]. A NULL leaves dst at T's zero value; any
// other value is converted by the standard library and assigned.
func (n nullScanner[T]) Scan(src any) error {
	var v stdsql.Null[T]
	if err := v.Scan(src); err != nil {
		return fmt.Errorf("null scan: %w", err)
	}
	*n.dst = v.V
	return nil
}
