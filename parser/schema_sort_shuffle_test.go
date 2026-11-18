package parser_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
)

// rdwSortFixture lists (schema, name) pairs whose schema-first order differs
// from their name-first order: schema b holds a, schema a holds z.
var rdwSortFixture = [][2]string{{"b", "a"}, {"a", "z"}, {"b", "y"}, {"a", "m"}}

// rdwSortWant is rdwSortFixture sorted by schema, then name.
var rdwSortWant = []string{"a.m", "a.z", "b.a", "b.y"}

// rdwPermutations returns every order of the indexes 0..n-1.
func rdwPermutations(n int) [][]int {
	if n == 0 {
		return [][]int{{}}
	}
	var out [][]int
	for _, p := range rdwPermutations(n - 1) {
		for i := 0; i <= len(p); i++ {
			q := append(append(append([]int{}, p[:i]...), n-1), p[i:]...)
			out = append(out, q)
		}
	}
	return out
}

// Schema.Sort orders tables, enums, sets, composite types and domain types by
// schema, then name, whatever order they arrive in, and leaves each table's
// columns in their declared order.
func TestSchemaSort_SchemaThenName(t *testing.T) {
	tests := []struct {
		name  string
		build func(order []int) *parser.Schema
		got   func(s *parser.Schema) []string
	}{
		{
			name: "tables",
			build: func(order []int) *parser.Schema {
				s := &parser.Schema{}
				for _, i := range order {
					f := rdwSortFixture[i]
					s.Tables = append(s.Tables, parser.Table{Schema: f[0], Name: f[1]})
				}
				return s
			},
			got: func(s *parser.Schema) []string {
				out := make([]string, 0, len(s.Tables))
				for _, x := range s.Tables {
					out = append(out, x.Schema+"."+x.Name)
				}
				return out
			},
		},
		{
			name: "enums",
			build: func(order []int) *parser.Schema {
				s := &parser.Schema{}
				for _, i := range order {
					f := rdwSortFixture[i]
					s.Enums = append(s.Enums, parser.Enum{Schema: f[0], Name: f[1]})
				}
				return s
			},
			got: func(s *parser.Schema) []string {
				out := make([]string, 0, len(s.Enums))
				for _, x := range s.Enums {
					out = append(out, x.Schema+"."+x.Name)
				}
				return out
			},
		},
		{
			name: "MySQL sets",
			build: func(order []int) *parser.Schema {
				s := &parser.Schema{}
				for _, i := range order {
					f := rdwSortFixture[i]
					s.Sets = append(s.Sets, parser.Set{Schema: f[0], Name: f[1]})
				}
				return s
			},
			got: func(s *parser.Schema) []string {
				out := make([]string, 0, len(s.Sets))
				for _, x := range s.Sets {
					out = append(out, x.Schema+"."+x.Name)
				}
				return out
			},
		},
		{
			name: "composite types",
			build: func(order []int) *parser.Schema {
				s := &parser.Schema{}
				for _, i := range order {
					f := rdwSortFixture[i]
					s.CompositeTypes = append(s.CompositeTypes, parser.CompositeType{Schema: f[0], Name: f[1]})
				}
				return s
			},
			got: func(s *parser.Schema) []string {
				out := make([]string, 0, len(s.CompositeTypes))
				for _, x := range s.CompositeTypes {
					out = append(out, x.Schema+"."+x.Name)
				}
				return out
			},
		},
		{
			name: "domain types",
			build: func(order []int) *parser.Schema {
				s := &parser.Schema{}
				for _, i := range order {
					f := rdwSortFixture[i]
					s.DomainTypes = append(s.DomainTypes, parser.DomainType{Schema: f[0], Name: f[1]})
				}
				return s
			},
			got: func(s *parser.Schema) []string {
				out := make([]string, 0, len(s.DomainTypes))
				for _, x := range s.DomainTypes {
					out = append(out, x.Schema+"."+x.Name)
				}
				return out
			},
		},
	}
	orders := rdwPermutations(len(rdwSortFixture))
	if len(orders) != 24 {
		t.Fatalf("rdwPermutations(4) gave %d orders, want 24", len(orders))
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, order := range orders {
				s := tt.build(order)
				s.Sort()
				if diff := cmp.Diff(rdwSortWant, tt.got(s)); diff != "" {
					t.Errorf("Sort() of input order %v mismatch (-want +got):\n%s", order, diff)
				}
			}
		})
	}
}

// Sorting the tables leaves every table's columns in declared order.
func TestSchemaSort_ColumnsKeepDeclaredOrder(t *testing.T) {
	columns := func(names ...string) []parser.Column {
		cols := make([]parser.Column, len(names))
		for i, n := range names {
			cols[i] = parser.Column{Name: n}
		}
		return cols
	}
	declared := map[string][]string{
		"b.a": {"zeta", "alpha", "mid"},
		"a.z": {"y", "b", "x", "a"},
		"b.y": {"c", "a", "b"},
		"a.m": {"z", "a"},
	}
	for _, order := range rdwPermutations(len(rdwSortFixture)) {
		s := &parser.Schema{}
		for _, i := range order {
			f := rdwSortFixture[i]
			s.Tables = append(s.Tables, parser.Table{Schema: f[0], Name: f[1], Columns: columns(declared[f[0]+"."+f[1]]...)})
		}
		s.Sort()
		gotTables := make([]string, 0, len(s.Tables))
		got := map[string][]string{}
		for _, tbl := range s.Tables {
			key := tbl.Schema + "." + tbl.Name
			gotTables = append(gotTables, key)
			for _, c := range tbl.Columns {
				got[key] = append(got[key], c.Name)
			}
		}
		if diff := cmp.Diff(rdwSortWant, gotTables); diff != "" {
			t.Errorf("Sort() of input order %v: tables mismatch (-want +got):\n%s", order, diff)
		}
		if diff := cmp.Diff(declared, got); diff != "" {
			t.Errorf("Sort() of input order %v: columns mismatch (-want +got):\n%s", order, diff)
		}
	}
}
