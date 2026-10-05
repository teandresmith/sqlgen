package database_test

import (
	stdsql "database/sql"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/database"
)

// scannerType is a destination whose pointer implements sql.Scanner, standing
// in for the UUID and decimal types a target PK resolves to. NullScan must
// route a non-NULL value through it rather than around it.
type scannerType struct {
	got string
}

func (s *scannerType) Scan(src any) error {
	s.got = "scanned:" + src.(string)
	return nil
}

// TestNullScan covers both halves of the wrapper: a NULL leaves the destination
// at its zero value instead of failing, and a value is converted exactly as an
// unwrapped destination would convert it.
func TestNullScan(t *testing.T) {
	t.Run("null reads as the zero value", func(t *testing.T) {
		// Each destination starts non-zero, so a wrapper that never assigned
		// would fail these just as surely as one that errored.
		i := int64(7)
		s := "set"
		b := true
		m := time.Unix(1, 0)

		// got reads its destination when the assertion runs, after Scan — a
		// method value such as m.IsZero would bind a copy taken before it.
		for _, tt := range []struct {
			name string
			dest any
			got  func() any
			want any
		}{
			{"int64", database.NullScan(&i), func() any { return i }, int64(0)},
			{"string", database.NullScan(&s), func() any { return s }, ""},
			{"bool", database.NullScan(&b), func() any { return b }, false},
			{"time", database.NullScan(&m), func() any { return m }, time.Time{}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				sc, ok := tt.dest.(stdsql.Scanner)
				if !ok {
					t.Fatalf("NullScan(%s) does not implement sql.Scanner", tt.name)
				}
				if err := sc.Scan(nil); err != nil {
					t.Fatalf("Scan(nil) = %v, want nil", err)
				}
				if got := tt.got(); got != tt.want {
					t.Errorf("Scan(nil) left %s = %v, want %v", tt.name, got, tt.want)
				}
			})
		}
	})

	t.Run("value converts as usual", func(t *testing.T) {
		var i int64
		if err := database.NullScan(&i).(stdsql.Scanner).Scan(int64(42)); err != nil {
			t.Fatalf("Scan(42) = %v, want nil", err)
		}
		if i != 42 {
			t.Errorf("i = %d, want 42", i)
		}
	})

	t.Run("value routes through a Scanner destination", func(t *testing.T) {
		var v scannerType
		if err := database.NullScan(&v).(stdsql.Scanner).Scan("abc"); err != nil {
			t.Fatalf("Scan(\"abc\") = %v, want nil", err)
		}
		if v.got != "scanned:abc" {
			t.Errorf("got = %q, want %q — the destination's own Scan was bypassed", v.got, "scanned:abc")
		}
	})

	t.Run("a conversion error still surfaces", func(t *testing.T) {
		var i int64
		if err := database.NullScan(&i).(stdsql.Scanner).Scan("not a number"); err == nil {
			t.Error("Scan(\"not a number\") into int64 = nil, want an error")
		}
	})
}
