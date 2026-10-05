package comparator_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/sql"
)

func TestTime_Parse(t *testing.T) {
	now := time.Date(2025, 1, 15, 9, 30, 0, 0, time.UTC)
	later := time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC)

	tests := []struct {
		name string
		comp comparator.Time
		want []sql.Condition
	}{
		{
			name: "Eq",
			comp: comparator.Time{Eq: &now},
			want: []sql.Condition{{Clause: "created_at = $", Value: now, Column: "created_at"}},
		},
		{
			name: "Gt",
			comp: comparator.Time{Gt: &now},
			want: []sql.Condition{{Clause: "created_at > $", Value: now, Column: "created_at"}},
		},
		{
			name: "Between",
			comp: comparator.Time{Between: &comparator.Range[time.Time]{Start: now, End: later}},
			want: []sql.Condition{{Clause: "created_at BETWEEN $ AND $", Value: sql.Range{Start: now, End: later}, Column: "created_at"}},
		},
		{
			name: "NBetween",
			comp: comparator.Time{NBetween: &comparator.Range[time.Time]{Start: now, End: later}},
			want: []sql.Condition{{Clause: "created_at NOT BETWEEN $ AND $", Value: sql.Range{Start: now, End: later}, Column: "created_at"}},
		},
		{
			name: "Custom passthrough",
			comp: comparator.Time{Custom: []sql.Condition{{Clause: "created_at::date = $", Value: "2025-01-15"}}},
			want: []sql.Condition{{Clause: "created_at::date = $", Value: "2025-01-15"}},
		},
		{
			name: "empty produces nil",
			comp: comparator.Time{},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.comp.Parse("created_at", pgDialect)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Time.Parse() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTime_Parse_In_DialectAware(t *testing.T) {
	t1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	comp := comparator.Time{In: []time.Time{t1, t2}}

	t.Run("postgres pgx uses ANY", func(t *testing.T) {
		got := comp.Parse("ts", pgDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "ts = ANY($)" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "ts = ANY($)")
		}
	})

	t.Run("postgres stdlib uses IN", func(t *testing.T) {
		got := comp.Parse("ts", pgStdDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "ts IN $" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "ts IN $")
		}
	})

	t.Run("mysql uses IN", func(t *testing.T) {
		got := comp.Parse("ts", mysqlDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "ts IN $" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "ts IN $")
		}
	})

	t.Run("sqlite uses IN", func(t *testing.T) {
		got := comp.Parse("ts", sqliteDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "ts IN $" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "ts IN $")
		}
	})
}

func TestNullableTime_Parse(t *testing.T) {
	t.Run("Null true", func(t *testing.T) {
		comp := comparator.NullableTime{Null: new(true)}
		got := comp.Parse("deleted_at", pgDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "deleted_at IS NULL" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "deleted_at IS NULL")
		}
	})

	t.Run("inherits Time operators", func(t *testing.T) {
		now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		comp := comparator.NullableTime{
			Time: comparator.Time{Gt: &now},
			Null: new(false),
		}
		got := comp.Parse("deleted_at", pgDialect)
		if len(got) != 2 {
			t.Fatalf("got %d conditions, want 2", len(got))
		}
		if got[0].Clause != "deleted_at > $" {
			t.Errorf("got[0].Clause = %q, want %q", got[0].Clause, "deleted_at > $")
		}
		if got[1].Clause != "deleted_at IS NOT NULL" {
			t.Errorf("got[1].Clause = %q, want %q", got[1].Clause, "deleted_at IS NOT NULL")
		}
	})
}

// TestTime_Parse_Nin_DialectAware pins the cross-dialect SQL shape of the `nin`
// operator, which the GraphQL surface exposes for the first time (PRD §26.4).
// `in` was already swept above; `nin` takes the mirrored ALL / NOT IN split and
// had no dialect coverage.
func TestTime_Parse_Nin_DialectAware(t *testing.T) {
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	later := now.Add(24 * time.Hour)
	comp := comparator.Time{Nin: []time.Time{now, later}}

	tests := []struct {
		name    string
		dialect sql.Dialect
		want    string
	}{
		{name: "postgres pgx uses ALL", dialect: pgDialect, want: "created_at != ALL($)"},
		{name: "postgres stdlib uses NOT IN", dialect: pgStdDialect, want: "created_at NOT IN $"},
		{name: "mysql uses NOT IN", dialect: mysqlDialect, want: "created_at NOT IN $"},
		{name: "sqlite uses NOT IN", dialect: sqliteDialect, want: "created_at NOT IN $"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := comp.Parse("created_at", tt.dialect)
			if len(got) != 1 {
				t.Fatalf("got %d conditions, want 1", len(got))
			}
			if got[0].Clause != tt.want {
				t.Errorf("clause = %q, want %q", got[0].Clause, tt.want)
			}
		})
	}
}
