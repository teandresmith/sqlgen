package comparator_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/sql"
)

func TestJSONB_Parse(t *testing.T) {
	containsVal := any(`{"status":"active"}`)
	containedByVal := any(`{"status":"active","role":"admin"}`)

	tests := []struct {
		name string
		comp comparator.JSONB
		want []sql.Condition
	}{
		{
			name: "HasKey",
			comp: comparator.JSONB{HasKey: new("name")},
			want: []sql.Condition{{Clause: "data ? $", Value: "name", Column: "data"}},
		},
		{
			name: "HasAnyKey",
			comp: comparator.JSONB{HasAnyKey: []string{"name", "email"}},
			want: []sql.Condition{{Clause: "data ?| $", Value: []string{"name", "email"}, Column: "data"}},
		},
		{
			name: "HasAllKeys",
			comp: comparator.JSONB{HasAllKeys: []string{"name", "email"}},
			want: []sql.Condition{{Clause: "data ?& $", Value: []string{"name", "email"}, Column: "data"}},
		},
		{
			name: "Contains",
			comp: comparator.JSONB{Contains: &containsVal},
			want: []sql.Condition{{Clause: "data @> $", Value: `{"status":"active"}`, Column: "data"}},
		},
		{
			name: "ContainedBy",
			comp: comparator.JSONB{ContainedBy: &containedByVal},
			want: []sql.Condition{{Clause: "data <@ $", Value: `{"status":"active","role":"admin"}`, Column: "data"}},
		},
		{
			name: "PathExists",
			comp: comparator.JSONB{PathExists: new("$.items[*].price")},
			want: []sql.Condition{{Clause: "data @? $", Value: "$.items[*].price", Column: "data"}},
		},
		{
			name: "Custom passthrough",
			comp: comparator.JSONB{Custom: []sql.Condition{{Clause: "data #>> '{a,b}' = $", Value: "val"}}},
			want: []sql.Condition{{Clause: "data #>> '{a,b}' = $", Value: "val"}},
		},
		{
			name: "empty produces nil",
			comp: comparator.JSONB{},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.comp.Parse("data", pgDialect)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("JSONB.Parse() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestJSONB_Parse_MultipleOperators(t *testing.T) {
	containsVal := any(`{"a":1}`)
	comp := comparator.JSONB{
		HasKey:   new("name"),
		Contains: &containsVal,
	}
	got := comp.Parse("data", pgDialect)
	if len(got) != 2 {
		t.Fatalf("got %d conditions, want 2", len(got))
	}
	if got[0].Clause != "data ? $" {
		t.Errorf("got[0].Clause = %q, want %q", got[0].Clause, "data ? $")
	}
	if got[1].Clause != "data @> $" {
		t.Errorf("got[1].Clause = %q, want %q", got[1].Clause, "data @> $")
	}
}

func TestNullableJSONB_Parse(t *testing.T) {
	for _, tt := range []struct {
		name string
		null bool
		want sql.Condition
	}{
		{"Null true", true, sql.Condition{Clause: "data IS NULL", Column: "data"}},
		{"Null false", false, sql.Condition{Clause: "data IS NOT NULL", Column: "data"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := comparator.NullableJSONB{Null: new(tt.null)}.Parse("data", pgDialect)
			if diff := cmp.Diff([]sql.Condition{tt.want}, got); diff != "" {
				t.Errorf("NullableJSONB{Null: %t}.Parse() mismatch (-want +got):\n%s", tt.null, diff)
			}
		})
	}

	t.Run("inherits JSONB operators", func(t *testing.T) {
		comp := comparator.NullableJSONB{
			JSONB: comparator.JSONB{HasKey: new("key")},
			Null:  new(false),
		}
		got := comp.Parse("data", pgDialect)
		if len(got) != 2 {
			t.Fatalf("got %d conditions, want 2", len(got))
		}
		if got[0].Clause != "data ? $" {
			t.Errorf("got[0].Clause = %q, want %q", got[0].Clause, "data ? $")
		}
		if got[1].Clause != "data IS NOT NULL" {
			t.Errorf("got[1].Clause = %q, want %q", got[1].Clause, "data IS NOT NULL")
		}
	})
}
