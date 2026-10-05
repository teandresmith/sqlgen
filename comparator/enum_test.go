package comparator_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/sql"
)

func TestEnum_Parse(t *testing.T) {
	tests := []struct {
		name string
		comp comparator.Enum[string]
		want []sql.Condition
	}{
		{
			name: "Eq",
			comp: comparator.Enum[string]{Eq: new("active")},
			want: []sql.Condition{{Clause: "status = $", Value: "active", Column: "status"}},
		},
		{
			name: "Neq",
			comp: comparator.Enum[string]{Neq: new("deleted")},
			want: []sql.Condition{{Clause: "status != $", Value: "deleted", Column: "status"}},
		},
		{
			name: "Custom passthrough",
			comp: comparator.Enum[string]{Custom: []sql.Condition{{Clause: "status = $", Value: "custom"}}},
			want: []sql.Condition{{Clause: "status = $", Value: "custom"}},
		},
		{
			name: "empty produces nil",
			comp: comparator.Enum[string]{},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.comp.Parse("status", pgDialect)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Enum[string].Parse() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestEnum_Parse_In_DialectAware(t *testing.T) {
	comp := comparator.Enum[string]{In: []string{"active", "pending", "review"}}

	// Unlike every other family, Enum takes the EXPANDED form on pgx too —
	// pgx cannot encode a slice of a named enum type against the column's
	// enum-array OID. See the comment on Enum.Parse.
	t.Run("postgres pgx uses IN", func(t *testing.T) {
		got := comp.Parse("status", pgDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "status IN $" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "status IN $")
		}
		want := []any{"active", "pending", "review"}
		if diff := cmp.Diff(want, got[0].Value); diff != "" {
			t.Errorf("value mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("postgres stdlib uses IN", func(t *testing.T) {
		got := comp.Parse("status", pgStdDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "status IN $" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "status IN $")
		}
		want := []any{"active", "pending", "review"}
		if diff := cmp.Diff(want, got[0].Value); diff != "" {
			t.Errorf("value mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("mysql uses IN", func(t *testing.T) {
		got := comp.Parse("status", mysqlDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "status IN $" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "status IN $")
		}
		want := []any{"active", "pending", "review"}
		if diff := cmp.Diff(want, got[0].Value); diff != "" {
			t.Errorf("value mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("sqlite uses IN", func(t *testing.T) {
		got := comp.Parse("status", sqliteDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "status IN $" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "status IN $")
		}
		want := []any{"active", "pending", "review"}
		if diff := cmp.Diff(want, got[0].Value); diff != "" {
			t.Errorf("value mismatch (-want +got):\n%s", diff)
		}
	})
}

func TestNullableEnum_Parse(t *testing.T) {
	t.Run("Null true", func(t *testing.T) {
		comp := comparator.NullableEnum[string]{Null: new(true)}
		got := comp.Parse("role", pgDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "role IS NULL" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "role IS NULL")
		}
	})

	t.Run("inherits Enum operators", func(t *testing.T) {
		comp := comparator.NullableEnum[string]{
			Enum: comparator.Enum[string]{Eq: new("admin")},
			Null: new(false),
		}
		got := comp.Parse("role", pgDialect)
		if len(got) != 2 {
			t.Fatalf("got %d conditions, want 2", len(got))
		}
		if got[0].Clause != "role = $" {
			t.Errorf("got[0].Clause = %q, want %q", got[0].Clause, "role = $")
		}
		if got[1].Clause != "role IS NOT NULL" {
			t.Errorf("got[1].Clause = %q, want %q", got[1].Clause, "role IS NOT NULL")
		}
	})
}

// orderStatus is a named string type, the shape sqlgen generates for every
// schema enum (`type OrderStatus string`).
type orderStatus string

// enumRank is the other shape Enum[T] can be instantiated with: an
// enum-shaped column retyped to an integer through `overrides.types`.
// resolveGenericComparator still routes it through Enum[T], so the set
// operators must keep working without assuming a string.
type enumRank int32

// TestEnum_Parse_SetOperandsAreDriverEncodable pins the fix for the pgx
// enum-array encode failure. Every other comparator hands pgx the whole slice
// as one array parameter, and pgx has no encode plan for a slice of a NAMED
// enum type against the column's enum-array OID — `= ANY($1)` failed the
// query outright.
//
// `Eq` was unaffected, since a scalar named value goes through the driver's
// kind-based conversion, which is why every existing enum filter test passed
// while In and Nin were unreachable. Enum therefore expands to
// `IN ($, $, …)` on ALL four dialects: it asks the driver for nothing beyond
// what `Eq` already proved it can do.
func TestEnum_Parse_SetOperandsAreDriverEncodable(t *testing.T) {
	comp := comparator.Enum[orderStatus]{
		In:  []orderStatus{"shipped", "delivered"},
		Nin: []orderStatus{"pending"},
	}

	for _, tt := range []struct {
		name    string
		dialect sql.Dialect
	}{
		{"postgres pgx", pgDialect},
		{"postgres stdlib", pgStdDialect},
		{"mysql", mysqlDialect},
		{"sqlite", sqliteDialect},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := comp.Parse("status", tt.dialect)
			if len(got) != 2 {
				t.Fatalf("got %d conditions, want 2", len(got))
			}
			if got[0].Clause != "status IN $" {
				t.Errorf("In clause = %q, want %q", got[0].Clause, "status IN $")
			}
			if diff := cmp.Diff([]any{orderStatus("shipped"), orderStatus("delivered")}, got[0].Value); diff != "" {
				t.Errorf("In value mismatch (-want +got):\n%s", diff)
			}
			if got[1].Clause != "status NOT IN $" {
				t.Errorf("Nin clause = %q, want %q", got[1].Clause, "status NOT IN $")
			}
			if diff := cmp.Diff([]any{orderStatus("pending")}, got[1].Value); diff != "" {
				t.Errorf("Nin value mismatch (-want +got):\n%s", diff)
			}
		})
	}

	// A non-string enum type takes the identical path — the fix keeps
	// Enum[T]'s constraint at `comparable` rather than narrowing it to
	// `~string`, so an enum-shaped column retyped to an integer still
	// compiles and still filters.
	t.Run("non-string enum type", func(t *testing.T) {
		ranked := comparator.Enum[enumRank]{In: []enumRank{2, 5}}
		got := ranked.Parse("rank", pgDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if diff := cmp.Diff([]any{enumRank(2), enumRank(5)}, got[0].Value); diff != "" {
			t.Errorf("value mismatch (-want +got):\n%s", diff)
		}
	})
}
