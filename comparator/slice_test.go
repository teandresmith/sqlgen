package comparator_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/sql"
)

func TestSlice_Parse(t *testing.T) {
	tests := []struct {
		name string
		comp comparator.Slice[string]
		want []sql.Condition
	}{
		{
			name: "ContainsAny",
			comp: comparator.Slice[string]{ContainsAny: []string{"a", "b"}},
			want: []sql.Condition{{Clause: "tags && $", Value: "{a,b}", Column: "tags"}},
		},
		{
			name: "ContainsAll",
			comp: comparator.Slice[string]{ContainsAll: []string{"x", "y"}},
			want: []sql.Condition{{Clause: "tags @> $", Value: "{x,y}", Column: "tags"}},
		},
		{
			name: "ContainedBy",
			comp: comparator.Slice[string]{ContainedBy: []string{"a", "b", "c"}},
			want: []sql.Condition{{Clause: "tags <@ $", Value: "{a,b,c}", Column: "tags"}},
		},
		{
			name: "IsEmpty true",
			comp: comparator.Slice[string]{IsEmpty: new(true)},
			want: []sql.Condition{{Clause: "tags = '{}'", Value: nil, Column: "tags"}},
		},
		{
			name: "IsEmpty false",
			comp: comparator.Slice[string]{IsEmpty: new(false)},
			want: []sql.Condition{{Clause: "tags != '{}'", Value: nil, Column: "tags"}},
		},
		{
			name: "Custom passthrough",
			comp: comparator.Slice[string]{Custom: []sql.Condition{{Clause: "array_length(tags, 1) > $", Value: 5}}},
			want: []sql.Condition{{Clause: "array_length(tags, 1) > $", Value: 5}},
		},
		{
			name: "empty produces nil",
			comp: comparator.Slice[string]{},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.comp.Parse("tags", pgDialect)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Slice[string].Parse() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSlice_Parse_MultipleOperators(t *testing.T) {
	comp := comparator.Slice[string]{
		ContainsAny: []string{"go", "rust"},
		ContainsAll: []string{"backend"},
	}
	got := comp.Parse("tags", pgDialect)
	if len(got) != 2 {
		t.Fatalf("got %d conditions, want 2", len(got))
	}
	if got[0].Clause != "tags && $" {
		t.Errorf("got[0].Clause = %q, want %q", got[0].Clause, "tags && $")
	}
	if got[1].Clause != "tags @> $" {
		t.Errorf("got[1].Clause = %q, want %q", got[1].Clause, "tags @> $")
	}
}

func TestNullableSlice_Parse(t *testing.T) {
	t.Run("Null true", func(t *testing.T) {
		comp := comparator.NullableSlice[string]{Null: new(true)}
		got := comp.Parse("tags", pgDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "tags IS NULL" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "tags IS NULL")
		}
	})

	t.Run("inherits Slice operators", func(t *testing.T) {
		comp := comparator.NullableSlice[string]{
			Slice: comparator.Slice[string]{ContainsAny: []string{"a"}},
			Null:  new(false),
		}
		got := comp.Parse("tags", pgDialect)
		if len(got) != 2 {
			t.Fatalf("got %d conditions, want 2", len(got))
		}
		if got[0].Clause != "tags && $" {
			t.Errorf("got[0].Clause = %q, want %q", got[0].Clause, "tags && $")
		}
		if got[1].Clause != "tags IS NOT NULL" {
			t.Errorf("got[1].Clause = %q, want %q", got[1].Clause, "tags IS NOT NULL")
		}
	})
}

// TestSlice_Parse_ArrayLiteralQuoting pins element quoting. The array literal
// is built by hand rather than encoded by the driver — that is deliberate, and
// it is what lets an ENUM array travel without OID registration — so this
// function owns PostgreSQL's array-literal grammar and has to honour it.
//
// The comma case is the one with teeth: an unquoted element carrying a comma
// splits into two on the server, so the predicate matches a DIFFERENT set and
// the query returns silently wrong rows. Braces and quotes fail loudly with
// `malformed array literal`; the empty string vanishes; a bare NULL is read as
// a NULL element rather than the four-character string.
func TestSlice_Parse_ArrayLiteralQuoting(t *testing.T) {
	tests := []struct {
		name string
		vals []string
		want string
	}{
		{
			name: "identifiers stay bare, matching PostgreSQL's own array_out",
			vals: []string{"admin", "editor"},
			want: "{admin,editor}",
		},
		{
			name: "enum members need no quoting — a dot is not special",
			vals: []string{"asset.primary", "spv"},
			want: "{asset.primary,spv}",
		},
		{
			name: "comma would split one element into two",
			vals: []string{"with,comma"},
			want: `{"with,comma"}`,
		},
		{
			name: "braces terminate the element early",
			vals: []string{"with{brace}"},
			want: `{"with{brace}"}`,
		},
		{
			name: "double quote is consumed as syntax and is escaped inside quotes",
			vals: []string{`with"quote`},
			want: `{"with\"quote"}`,
		},
		{
			name: "backslash is escaped too",
			vals: []string{`with\backslash`},
			want: `{"with\\backslash"}`,
		},
		{
			name: "whitespace is stripped when unquoted",
			vals: []string{" leading", "trailing ", "inner space"},
			want: `{" leading","trailing ","inner space"}`,
		},
		{
			name: "the empty string would vanish entirely",
			vals: []string{"", "after"},
			want: `{"",after}`,
		},
		{
			name: "a bare NULL would be read as a NULL element, any casing",
			vals: []string{"NULL", "null", "NuLl"},
			want: `{"NULL","null","NuLl"}`,
		},
		{
			name: "every operand shape is quoted, not just the first",
			vals: []string{"safe", "un,safe", "safe2"},
			want: `{safe,"un,safe",safe2}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// All three array operators share the literal, so asserting one
			// covers the encoding; the operator-to-clause mapping is pinned
			// by TestSlice_Parse above.
			got := comparator.Slice[string]{ContainsAny: tt.vals}.Parse("tags", pgDialect)
			if len(got) != 1 {
				t.Fatalf("got %d conditions, want 1", len(got))
			}
			if diff := cmp.Diff(tt.want, got[0].Value); diff != "" {
				t.Errorf("array literal mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestSlice_Parse_QuotingAppliesToEveryOperator guards against the fix landing
// on one operator and not its siblings — all three build the same literal.
func TestSlice_Parse_QuotingAppliesToEveryOperator(t *testing.T) {
	vals := []string{"un,safe"}
	const want = `{"un,safe"}`

	for _, tt := range []struct {
		name string
		comp comparator.Slice[string]
	}{
		{"ContainsAny", comparator.Slice[string]{ContainsAny: vals}},
		{"ContainsAll", comparator.Slice[string]{ContainsAll: vals}},
		{"ContainedBy", comparator.Slice[string]{ContainedBy: vals}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.comp.Parse("tags", pgDialect)
			if len(got) != 1 {
				t.Fatalf("got %d conditions, want 1", len(got))
			}
			if got[0].Value != want {
				t.Errorf("%s literal = %q, want %q", tt.name, got[0].Value, want)
			}
		})
	}
}
