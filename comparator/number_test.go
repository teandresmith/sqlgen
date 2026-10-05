package comparator_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/comparator"
	"github.com/teandresmith/sqlgen/sql"
)

func TestNumberInt_Parse(t *testing.T) {
	tests := []struct {
		name string
		comp comparator.Number[int]
		want []sql.Condition
	}{
		{
			name: "Eq",
			comp: comparator.Number[int]{Eq: new(42)},
			want: []sql.Condition{{Clause: "age = $", Value: 42, Column: "age"}},
		},
		{
			name: "Neq",
			comp: comparator.Number[int]{Neq: new(0)},
			want: []sql.Condition{{Clause: "age != $", Value: 0, Column: "age"}},
		},
		{
			name: "Gt",
			comp: comparator.Number[int]{Gt: new(18)},
			want: []sql.Condition{{Clause: "age > $", Value: 18, Column: "age"}},
		},
		{
			name: "Gte",
			comp: comparator.Number[int]{Gte: new(18)},
			want: []sql.Condition{{Clause: "age >= $", Value: 18, Column: "age"}},
		},
		{
			name: "Lt",
			comp: comparator.Number[int]{Lt: new(65)},
			want: []sql.Condition{{Clause: "age < $", Value: 65, Column: "age"}},
		},
		{
			name: "Lte",
			comp: comparator.Number[int]{Lte: new(65)},
			want: []sql.Condition{{Clause: "age <= $", Value: 65, Column: "age"}},
		},
		{
			name: "Between",
			comp: comparator.Number[int]{Between: &comparator.Range[int]{Start: 18, End: 65}},
			want: []sql.Condition{{Clause: "age BETWEEN $ AND $", Value: sql.Range{Start: 18, End: 65}, Column: "age"}},
		},
		{
			name: "NBetween",
			comp: comparator.Number[int]{NBetween: &comparator.Range[int]{Start: 0, End: 10}},
			want: []sql.Condition{{Clause: "age NOT BETWEEN $ AND $", Value: sql.Range{Start: 0, End: 10}, Column: "age"}},
		},
		{
			name: "Custom passthrough",
			comp: comparator.Number[int]{Custom: []sql.Condition{{Clause: "age % $ = 0", Value: 2}}},
			want: []sql.Condition{{Clause: "age % $ = 0", Value: 2}},
		},
		{
			name: "empty produces nil",
			comp: comparator.Number[int]{},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.comp.Parse("age", pgDialect)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Number[int].Parse() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNumberFloat64_Parse(t *testing.T) {
	tests := []struct {
		name string
		comp comparator.Number[float64]
		want []sql.Condition
	}{
		{
			name: "Eq",
			comp: comparator.Number[float64]{Eq: new(9.99)},
			want: []sql.Condition{{Clause: "price = $", Value: 9.99, Column: "price"}},
		},
		{
			name: "Between",
			comp: comparator.Number[float64]{Between: &comparator.Range[float64]{Start: 0.01, End: 999.99}},
			want: []sql.Condition{{Clause: "price BETWEEN $ AND $", Value: sql.Range{Start: 0.01, End: 999.99}, Column: "price"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.comp.Parse("price", pgDialect)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Number[float64].Parse() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNumberInt_Parse_In_DialectAware(t *testing.T) {
	comp := comparator.Number[int]{In: []int{1, 2, 3}}

	t.Run("postgres pgx uses ANY", func(t *testing.T) {
		got := comp.Parse("id", pgDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "id = ANY($)" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "id = ANY($)")
		}
		want := []int{1, 2, 3}
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
		want := []any{1, 2, 3}
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
		want := []any{1, 2, 3}
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
		want := []any{1, 2, 3}
		if diff := cmp.Diff(want, got[0].Value); diff != "" {
			t.Errorf("value mismatch (-want +got):\n%s", diff)
		}
	})
}

func TestNumberInt_Parse_MultipleOperators(t *testing.T) {
	comp := comparator.Number[int]{
		Gte:     new(18),
		Lt:      new(65),
		Between: &comparator.Range[int]{Start: 25, End: 35},
	}
	got := comp.Parse("age", pgDialect)
	if len(got) != 3 {
		t.Fatalf("got %d conditions, want 3", len(got))
	}
	if got[0].Clause != "age >= $" {
		t.Errorf("got[0].Clause = %q, want %q", got[0].Clause, "age >= $")
	}
	if got[1].Clause != "age < $" {
		t.Errorf("got[1].Clause = %q, want %q", got[1].Clause, "age < $")
	}
	if got[2].Clause != "age BETWEEN $ AND $" {
		t.Errorf("got[2].Clause = %q, want %q", got[2].Clause, "age BETWEEN $ AND $")
	}
}

func TestNullableNumber_Parse(t *testing.T) {
	t.Run("Null true", func(t *testing.T) {
		comp := comparator.NullableNumber[int]{Null: new(true)}
		got := comp.Parse("score", pgDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "score IS NULL" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "score IS NULL")
		}
	})

	t.Run("Null false", func(t *testing.T) {
		comp := comparator.NullableNumber[int]{Null: new(false)}
		got := comp.Parse("score", pgDialect)
		if len(got) != 1 {
			t.Fatalf("got %d conditions, want 1", len(got))
		}
		if got[0].Clause != "score IS NOT NULL" {
			t.Errorf("clause = %q, want %q", got[0].Clause, "score IS NOT NULL")
		}
	})

	t.Run("inherits Number operators", func(t *testing.T) {
		comp := comparator.NullableNumber[int]{
			Number: comparator.Number[int]{Gte: new(10)},
			Null:   new(false),
		}
		got := comp.Parse("score", pgDialect)
		if len(got) != 2 {
			t.Fatalf("got %d conditions, want 2", len(got))
		}
		if got[0].Clause != "score >= $" {
			t.Errorf("got[0].Clause = %q, want %q", got[0].Clause, "score >= $")
		}
		if got[1].Clause != "score IS NOT NULL" {
			t.Errorf("got[1].Clause = %q, want %q", got[1].Clause, "score IS NOT NULL")
		}
	})
}

// TestNumberInt_Parse_Nin_DialectAware pins the cross-dialect SQL shape of the
// `nin` operator, which the GraphQL surface exposes for the first time (PRD
// §26.4). `in` was already swept above; `nin` takes the mirrored ALL / NOT IN
// split and had no dialect coverage.
func TestNumberInt_Parse_Nin_DialectAware(t *testing.T) {
	comp := comparator.Number[int]{Nin: []int{1, 2}}

	tests := []struct {
		name    string
		dialect sql.Dialect
		want    string
	}{
		{name: "postgres pgx uses ALL", dialect: pgDialect, want: "stock != ALL($)"},
		{name: "postgres stdlib uses NOT IN", dialect: pgStdDialect, want: "stock NOT IN $"},
		{name: "mysql uses NOT IN", dialect: mysqlDialect, want: "stock NOT IN $"},
		{name: "sqlite uses NOT IN", dialect: sqliteDialect, want: "stock NOT IN $"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := comp.Parse("stock", tt.dialect)
			if len(got) != 1 {
				t.Fatalf("got %d conditions, want 1", len(got))
			}
			if got[0].Clause != tt.want {
				t.Errorf("clause = %q, want %q", got[0].Clause, tt.want)
			}
		})
	}
}
