package comparator_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/sql"
)

func TestString_Parse(t *testing.T) {
	tests := []struct {
		name string
		comp comparator.String
		want []sql.Condition
	}{
		{
			name: "Eq",
			comp: comparator.String{Eq: new("alice")},
			want: []sql.Condition{{Clause: "name = $", Value: "alice", Column: "name"}},
		},
		{
			name: "Neq",
			comp: comparator.String{Neq: new("bob")},
			want: []sql.Condition{{Clause: "name != $", Value: "bob", Column: "name"}},
		},
		{
			name: "Contains wraps with percent signs",
			comp: comparator.String{Contains: new("foo")},
			want: []sql.Condition{{Clause: "name LIKE $", Value: "%foo%", Column: "name"}},
		},
		{
			name: "StartsWith appends percent",
			comp: comparator.String{StartsWith: new("foo")},
			want: []sql.Condition{{Clause: "name LIKE $", Value: "foo%", Column: "name"}},
		},
		{
			name: "EndsWith prepends percent",
			comp: comparator.String{EndsWith: new("foo")},
			want: []sql.Condition{{Clause: "name LIKE $", Value: "%foo", Column: "name"}},
		},
		{
			name: "Like passes through",
			comp: comparator.String{Like: new("%widget%")},
			want: []sql.Condition{{Clause: "name LIKE $", Value: "%widget%", Column: "name"}},
		},
		{
			name: "NLike",
			comp: comparator.String{NLike: new("%test%")},
			want: []sql.Condition{{Clause: "name NOT LIKE $", Value: "%test%", Column: "name"}},
		},
		{
			name: "Gt",
			comp: comparator.String{Gt: new("aaa")},
			want: []sql.Condition{{Clause: "name > $", Value: "aaa", Column: "name"}},
		},
		{
			name: "Gte",
			comp: comparator.String{Gte: new("aaa")},
			want: []sql.Condition{{Clause: "name >= $", Value: "aaa", Column: "name"}},
		},
		{
			name: "Lt",
			comp: comparator.String{Lt: new("zzz")},
			want: []sql.Condition{{Clause: "name < $", Value: "zzz", Column: "name"}},
		},
		{
			name: "Lte",
			comp: comparator.String{Lte: new("zzz")},
			want: []sql.Condition{{Clause: "name <= $", Value: "zzz", Column: "name"}},
		},
		{
			name: "Custom passthrough",
			comp: comparator.String{Custom: []sql.Condition{{Clause: "name ~* $", Value: "pattern"}}},
			want: []sql.Condition{{Clause: "name ~* $", Value: "pattern"}},
		},
		{
			name: "empty produces nil",
			comp: comparator.String{},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.comp.Parse("name", pgDialect)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("String.Parse() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestString_Parse_In_DialectAware(t *testing.T) {
	comp := comparator.String{In: []string{"a", "b"}}

	t.Run("postgres pgx uses ANY", func(t *testing.T) {
		got := comp.Parse("name", pgDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "name = ANY($)" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "name = ANY($)")
		}
	})

	t.Run("postgres stdlib uses IN", func(t *testing.T) {
		got := comp.Parse("name", pgStdDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "name IN $" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "name IN $")
		}
	})

	t.Run("mysql uses IN", func(t *testing.T) {
		got := comp.Parse("name", mysqlDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "name IN $" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "name IN $")
		}
	})

	t.Run("sqlite uses IN", func(t *testing.T) {
		got := comp.Parse("name", sqliteDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "name IN $" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "name IN $")
		}
	})
}

func TestNullableString_Parse(t *testing.T) {
	t.Run("Null true", func(t *testing.T) {
		comp := comparator.NullableString{Null: new(true)}
		got := comp.Parse("bio", pgDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "bio IS NULL" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "bio IS NULL")
		}
	})

	t.Run("Null false", func(t *testing.T) {
		comp := comparator.NullableString{Null: new(false)}
		got := comp.Parse("bio", pgDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "bio IS NOT NULL" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "bio IS NOT NULL")
		}
	})

	t.Run("inherits String operators", func(t *testing.T) {
		comp := comparator.NullableString{
			String: comparator.String{Contains: new("test")},
			Null:   new(false),
		}
		got := comp.Parse("bio", pgDialect)
		if len(got) != 2 {
			t.Fatalf("got %d conditions, want 2", len(got))
		}
		if got[0].Clause != "bio LIKE $" {
			t.Errorf("got[0].Clause = %q, want %q", got[0].Clause, "bio LIKE $")
		}
		if got[0].Value != "%test%" {
			t.Errorf("got[0].Value = %v, want %q", got[0].Value, "%test%")
		}
	})
}

// TestString_Parse_Nin_DialectAware pins the cross-dialect SQL shape of the
// `nin` operator, which the GraphQL surface exposes for the first time (PRD
// §26.4). `in` was already swept above; `nin` takes the mirrored ALL / NOT IN
// split and had no dialect coverage.
func TestString_Parse_Nin_DialectAware(t *testing.T) {
	comp := comparator.String{Nin: []string{"a", "b"}}

	tests := []struct {
		name    string
		dialect sql.Dialect
		want    string
	}{
		{name: "postgres pgx uses ALL", dialect: pgDialect, want: "name != ALL($)"},
		{name: "postgres stdlib uses NOT IN", dialect: pgStdDialect, want: "name NOT IN $"},
		{name: "mysql uses NOT IN", dialect: mysqlDialect, want: "name NOT IN $"},
		{name: "sqlite uses NOT IN", dialect: sqliteDialect, want: "name NOT IN $"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := comp.Parse("name", tt.dialect)
			if len(got) != 1 {
				t.Fatalf("got %d conditions, want 1", len(got))
			}
			if got[0].Clause != tt.want {
				t.Errorf("clause = %q, want %q", got[0].Clause, tt.want)
			}
		})
	}
}
