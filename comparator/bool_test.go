package comparator_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/sql"
)

func TestBool_Parse(t *testing.T) {
	tests := []struct {
		name string
		comp comparator.Bool
		want []sql.Condition
	}{
		{
			name: "Eq true",
			comp: comparator.Bool{Eq: new(true)},
			want: []sql.Condition{{Clause: "active = $", Value: true, Column: "active"}},
		},
		{
			name: "Eq false",
			comp: comparator.Bool{Eq: new(false)},
			want: []sql.Condition{{Clause: "active = $", Value: false, Column: "active"}},
		},
		{
			name: "Neq",
			comp: comparator.Bool{Neq: new(true)},
			want: []sql.Condition{{Clause: "active != $", Value: true, Column: "active"}},
		},
		{
			name: "Custom passthrough",
			comp: comparator.Bool{Custom: []sql.Condition{{Clause: "active = $", Value: true}}},
			want: []sql.Condition{{Clause: "active = $", Value: true}},
		},
		{
			name: "empty produces nil",
			comp: comparator.Bool{},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.comp.Parse("active", pgDialect)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Bool.Parse() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNullableBool_Parse(t *testing.T) {
	t.Run("Null true", func(t *testing.T) {
		comp := comparator.NullableBool{Null: new(true)}
		got := comp.Parse("verified", pgDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "verified IS NULL" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "verified IS NULL")
		}
	})

	t.Run("Null false", func(t *testing.T) {
		comp := comparator.NullableBool{Null: new(false)}
		got := comp.Parse("verified", pgDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "verified IS NOT NULL" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "verified IS NOT NULL")
		}
	})
}
