package comparator_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/sql"
)

func TestID_Parse(t *testing.T) {
	tests := []struct {
		name string
		comp comparator.ID
		want []sql.Condition
	}{
		{
			name: "Eq",
			comp: comparator.ID{Eq: new("abc")},
			want: []sql.Condition{{Clause: "id = $", Value: "abc", Column: "id"}},
		},
		{
			name: "Neq",
			comp: comparator.ID{Neq: new("abc")},
			want: []sql.Condition{{Clause: "id != $", Value: "abc", Column: "id"}},
		},
		{
			name: "Gt",
			comp: comparator.ID{Gt: new("abc")},
			want: []sql.Condition{{Clause: "id > $", Value: "abc", Column: "id"}},
		},
		{
			name: "Gte",
			comp: comparator.ID{Gte: new("abc")},
			want: []sql.Condition{{Clause: "id >= $", Value: "abc", Column: "id"}},
		},
		{
			name: "Lt",
			comp: comparator.ID{Lt: new("abc")},
			want: []sql.Condition{{Clause: "id < $", Value: "abc", Column: "id"}},
		},
		{
			name: "Lte",
			comp: comparator.ID{Lte: new("abc")},
			want: []sql.Condition{{Clause: "id <= $", Value: "abc", Column: "id"}},
		},
		{
			name: "Custom passthrough",
			comp: comparator.ID{Custom: []sql.Condition{{Clause: "id ~ $", Value: "^abc"}}},
			want: []sql.Condition{{Clause: "id ~ $", Value: "^abc"}},
		},
		{
			name: "empty produces nil",
			comp: comparator.ID{},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.comp.Parse("id", pgDialect)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ID.Parse() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestID_Parse_In_DialectAware(t *testing.T) {
	comp := comparator.ID{In: []string{"a", "b", "c"}}

	t.Run("postgres pgx uses ANY", func(t *testing.T) {
		got := comp.Parse("id", pgDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "id = ANY($)" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "id = ANY($)")
		}
		want := []string{"a", "b", "c"}
		if diff := cmp.Diff(want, got[0].Value); diff != "" {
			t.Errorf("value mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("postgres stdlib uses IN", func(t *testing.T) {
		got := comp.Parse("id", pgStdDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "id IN $" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "id IN $")
		}
		want := []any{"a", "b", "c"}
		if diff := cmp.Diff(want, got[0].Value); diff != "" {
			t.Errorf("value mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("mysql uses IN", func(t *testing.T) {
		got := comp.Parse("id", mysqlDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "id IN $" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "id IN $")
		}
		want := []any{"a", "b", "c"}
		if diff := cmp.Diff(want, got[0].Value); diff != "" {
			t.Errorf("value mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("sqlite uses IN", func(t *testing.T) {
		got := comp.Parse("id", sqliteDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "id IN $" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "id IN $")
		}
		want := []any{"a", "b", "c"}
		if diff := cmp.Diff(want, got[0].Value); diff != "" {
			t.Errorf("value mismatch (-want +got):\n%s", diff)
		}
	})
}

func TestID_Parse_Nin_DialectAware(t *testing.T) {
	comp := comparator.ID{Nin: []string{"x", "y"}}

	t.Run("postgres pgx uses ALL", func(t *testing.T) {
		got := comp.Parse("id", pgDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "id != ALL($)" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "id != ALL($)")
		}
	})

	t.Run("postgres stdlib uses NOT IN", func(t *testing.T) {
		got := comp.Parse("id", pgStdDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "id NOT IN $" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "id NOT IN $")
		}
	})

	t.Run("mysql uses NOT IN", func(t *testing.T) {
		got := comp.Parse("id", mysqlDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "id NOT IN $" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "id NOT IN $")
		}
	})

	t.Run("sqlite uses NOT IN", func(t *testing.T) {
		got := comp.Parse("id", sqliteDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "id NOT IN $" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "id NOT IN $")
		}
	})
}

func TestID_Parse_MultipleOperators(t *testing.T) {
	comp := comparator.ID{
		Gt:  new("aaa"),
		Lte: new("zzz"),
	}
	got := comp.Parse("id", pgDialect)
	if len(got) != 2 {
		t.Fatalf("got %d conditions, want 2", len(got))
	}
	if got[0].Clause != "id > $" {
		t.Errorf("got[0].Clause = %q, want %q", got[0].Clause, "id > $")
	}
	if got[1].Clause != "id <= $" {
		t.Errorf("got[1].Clause = %q, want %q", got[1].Clause, "id <= $")
	}
}

func TestNullableID_Parse(t *testing.T) {
	t.Run("Null true produces IS NULL", func(t *testing.T) {
		comp := comparator.NullableID{Null: new(true)}
		got := comp.Parse("ref_id", pgDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "ref_id IS NULL" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "ref_id IS NULL")
		}
	})

	t.Run("Null false produces IS NOT NULL", func(t *testing.T) {
		comp := comparator.NullableID{Null: new(false)}
		got := comp.Parse("ref_id", pgDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "ref_id IS NOT NULL" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "ref_id IS NOT NULL")
		}
	})

	t.Run("inherits base ID operators", func(t *testing.T) {
		comp := comparator.NullableID{
			ID:   comparator.ID{Eq: new("abc")},
			Null: new(true),
		}
		got := comp.Parse("ref_id", pgDialect)
		if len(got) != 2 {
			t.Fatalf("got %d conditions, want 2", len(got))
		}
		if got[0].Clause != "ref_id = $" {
			t.Errorf("got[0].Clause = %q, want %q", got[0].Clause, "ref_id = $")
		}
		if got[1].Clause != "ref_id IS NULL" {
			t.Errorf("got[1].Clause = %q, want %q", got[1].Clause, "ref_id IS NULL")
		}
	})
}
