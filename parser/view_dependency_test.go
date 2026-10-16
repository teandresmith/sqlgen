package parser

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestCheckViewDependents(t *testing.T) {
	s := &Schema{ViewDependencies: []ViewDependency{
		{ViewSchema: "app", View: "v", Object: DependencyObject{Kind: ColumnDependency, Schema: "app", Name: "users", Part: "role"}},
		{ViewSchema: "app", View: "w", Object: DependencyObject{Kind: ConstraintDependency, Schema: "app", Name: "users", Part: "users_pkey"}},
		{ViewSchema: "app", View: "x", Object: DependencyObject{Kind: TableDependency, Schema: "app", Name: "tags"}},
		{ViewSchema: "app", View: "y", Object: DependencyObject{Kind: AttributeDependency, Schema: "app", Name: "addr_t", Part: "city"}},
		{ViewSchema: "app", View: "z", Object: DependencyObject{Kind: TypeDependency, Schema: "app", Name: "role_t"}},
	}}
	const stands = "; under source: both the database's view stands, so change it in the database first"
	tests := []struct {
		name, action string
		obj          DependencyObject
		want         string
	}{
		{"a column a view reads", "drop", DependencyObject{Kind: ColumnDependency, Schema: "app", Name: "users", Part: "role"}, "cannot drop column role of table app.users: view app.v depends on it" + stands},
		{"another column", "drop", DependencyObject{Kind: ColumnDependency, Schema: "app", Name: "users", Part: "email"}, ""},
		{"the column's type", "change the type of", DependencyObject{Kind: ColumnDependency, Schema: "app", Name: "users", Part: "role"}, "cannot change the type of column role of table app.users: view app.v depends on it" + stands},
		{"the table of a column a view reads", "drop", DependencyObject{Kind: TableDependency, Schema: "app", Name: "users"}, "cannot drop table app.users: view app.v depends on it" + stands},
		{"a table of another schema", "drop", DependencyObject{Kind: TableDependency, Schema: "other", Name: "users"}, ""},
		{"a constraint", "drop", DependencyObject{Kind: ConstraintDependency, Schema: "app", Name: "users", Part: "users_pkey"}, "cannot drop constraint users_pkey on table app.users: view app.w depends on it" + stands},
		{"a column of a table a view reads whole", "drop", DependencyObject{Kind: ColumnDependency, Schema: "app", Name: "tags", Part: "uid"}, ""},
		{"a table a view reads whole", "drop", DependencyObject{Kind: TableDependency, Schema: "app", Name: "tags"}, "cannot drop table app.tags: view app.x depends on it" + stands},
		{"an attribute", "drop", DependencyObject{Kind: AttributeDependency, Schema: "app", Name: "addr_t", Part: "city"}, "cannot drop attribute city of type app.addr_t: view app.y depends on it" + stands},
		{"the type of an attribute", "drop", DependencyObject{Kind: TypeDependency, Schema: "app", Name: "addr_t"}, "cannot drop type app.addr_t: view app.y depends on it" + stands},
		{"a type", "drop", DependencyObject{Kind: TypeDependency, Schema: "app", Name: "role_t"}, "cannot drop type app.role_t: view app.z depends on it" + stands},
		{"a table named like a type", "drop", DependencyObject{Kind: TableDependency, Schema: "app", Name: "role_t"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ""
			if err := CheckViewDependents(s, tt.action, tt.obj); err != nil {
				got = err.Error()
			}
			if got != tt.want {
				t.Errorf("CheckViewDependents() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenameDependencies(t *testing.T) {
	deps := func() []ViewDependency {
		return []ViewDependency{
			{View: "v", Object: DependencyObject{Kind: ColumnDependency, Schema: "app", Name: "users", Part: "role"}},
			{View: "v", Object: DependencyObject{Kind: ConstraintDependency, Schema: "app", Name: "users", Part: "users_pkey"}},
			{View: "v", Object: DependencyObject{Kind: TableDependency, Schema: "app", Name: "users"}},
			{View: "v", Object: DependencyObject{Kind: AttributeDependency, Schema: "app", Name: "users", Part: "role"}},
		}
	}
	tests := []struct {
		name     string
		from, to DependencyObject
		want     []DependencyObject
	}{
		{
			name: "a table, with its parts but not a type of its name",
			from: DependencyObject{Kind: TableDependency, Schema: "app", Name: "users"},
			to:   DependencyObject{Kind: TableDependency, Schema: "other", Name: "people"},
			want: []DependencyObject{
				{Kind: ColumnDependency, Schema: "other", Name: "people", Part: "role"},
				{Kind: ConstraintDependency, Schema: "other", Name: "people", Part: "users_pkey"},
				{Kind: TableDependency, Schema: "other", Name: "people"},
				{Kind: AttributeDependency, Schema: "app", Name: "users", Part: "role"},
			},
		},
		{
			name: "a column",
			from: DependencyObject{Kind: ColumnDependency, Schema: "app", Name: "users", Part: "role"},
			to:   DependencyObject{Kind: ColumnDependency, Schema: "app", Name: "users", Part: "kind"},
			want: []DependencyObject{
				{Kind: ColumnDependency, Schema: "app", Name: "users", Part: "kind"},
				{Kind: ConstraintDependency, Schema: "app", Name: "users", Part: "users_pkey"},
				{Kind: TableDependency, Schema: "app", Name: "users"},
				{Kind: AttributeDependency, Schema: "app", Name: "users", Part: "role"},
			},
		},
		{
			name: "a type, with its attributes",
			from: DependencyObject{Kind: TypeDependency, Schema: "app", Name: "users"},
			to:   DependencyObject{Kind: TypeDependency, Schema: "app", Name: "people_t"},
			want: []DependencyObject{
				{Kind: ColumnDependency, Schema: "app", Name: "users", Part: "role"},
				{Kind: ConstraintDependency, Schema: "app", Name: "users", Part: "users_pkey"},
				{Kind: TableDependency, Schema: "app", Name: "users"},
				{Kind: AttributeDependency, Schema: "app", Name: "people_t", Part: "role"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Schema{ViewDependencies: deps()}
			RenameDependencies(s, tt.from, tt.to)
			got := make([]DependencyObject, 0, len(s.ViewDependencies))
			for _, d := range s.ViewDependencies {
				got = append(got, d.Object)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("dependencies after the rename (-want +got):\n%s", diff)
			}
		})
	}
}
