package postgres_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/postgres"
)

// TestViewDependents pins that under source: both the parser refuses to drop
// or retype what a database view depends on, as PostgreSQL does, through a
// partition and after a rename too, and accepts what no view depends on.
func TestViewDependents(t *testing.T) {
	dep := func(kind parser.DependencyKind, name, part string) parser.ViewDependency {
		return parser.ViewDependency{ViewSchema: "public", View: "v", Object: parser.DependencyObject{Kind: kind, Schema: "public", Name: name, Part: part}}
	}
	tests := []struct {
		name string
		deps []parser.ViewDependency
		ddl  string
		// wantErr is part of the error; "" when the parser accepts the DDL.
		wantErr string
	}{
		{
			name:    "drop a column",
			deps:    []parser.ViewDependency{dep(parser.ColumnDependency, "users", "role")},
			ddl:     `ALTER TABLE users DROP COLUMN role;`,
			wantErr: "cannot drop column role of table public.users: view public.v depends on it",
		},
		{
			name: "drop a column no view depends on",
			deps: []parser.ViewDependency{dep(parser.ColumnDependency, "users", "role")},
			ddl:  `ALTER TABLE users DROP COLUMN email;`,
		},
		{
			name:    "drop a column under CASCADE",
			deps:    []parser.ViewDependency{dep(parser.ColumnDependency, "users", "role")},
			ddl:     `ALTER TABLE users DROP COLUMN role CASCADE;`,
			wantErr: "cannot drop column role of table public.users",
		},
		{
			name:    "change a column's type",
			deps:    []parser.ViewDependency{dep(parser.ColumnDependency, "users", "role")},
			ddl:     `ALTER TABLE users ALTER COLUMN role TYPE text;`,
			wantErr: "cannot change the type of column role of table public.users",
		},
		{
			name:    "drop a constraint",
			deps:    []parser.ViewDependency{dep(parser.ConstraintDependency, "users", "users_pkey")},
			ddl:     `ALTER TABLE users DROP CONSTRAINT users_pkey;`,
			wantErr: "cannot drop constraint users_pkey on table public.users",
		},
		{
			name:    "drop a table",
			deps:    []parser.ViewDependency{dep(parser.TableDependency, "users", "")},
			ddl:     `DROP TABLE users;`,
			wantErr: "cannot drop table public.users",
		},
		{
			name:    "drop a table a view depends on a column of",
			deps:    []parser.ViewDependency{dep(parser.ColumnDependency, "users", "role")},
			ddl:     `DROP TABLE users;`,
			wantErr: "cannot drop table public.users",
		},
		{
			name:    "drop a type",
			deps:    []parser.ViewDependency{dep(parser.TypeDependency, "role_t", "")},
			ddl:     `DROP TYPE role_t;`,
			wantErr: "cannot drop type public.role_t",
		},
		{
			name:    "drop a partitioned table's column a partition's view depends on",
			deps:    []parser.ViewDependency{dep(parser.ColumnDependency, "events_2024", "x")},
			ddl:     `ALTER TABLE events DROP COLUMN x;`,
			wantErr: "cannot drop column x of table public.events_2024",
		},
		{
			name:    "rename a column, then drop it",
			deps:    []parser.ViewDependency{dep(parser.ColumnDependency, "users", "role")},
			ddl:     `ALTER TABLE users RENAME COLUMN role TO kind; ALTER TABLE users DROP COLUMN kind;`,
			wantErr: "cannot drop column kind of table public.users",
		},
		{
			name:    "rename a partitioned table's column, then drop it",
			deps:    []parser.ViewDependency{dep(parser.ColumnDependency, "events_2024", "x")},
			ddl:     `ALTER TABLE events RENAME COLUMN x TO y; ALTER TABLE events DROP COLUMN y;`,
			wantErr: "cannot drop column y of table public.events_2024",
		},
		{
			name:    "rename a table, then drop it",
			deps:    []parser.ViewDependency{dep(parser.TableDependency, "users", "")},
			ddl:     `ALTER TABLE users RENAME TO people; DROP TABLE people;`,
			wantErr: "cannot drop table public.people",
		},
		{
			name:    "drop a type under CASCADE",
			deps:    []parser.ViewDependency{dep(parser.TypeDependency, "role_t", "")},
			ddl:     `DROP TYPE role_t CASCADE;`,
			wantErr: "cannot drop type public.role_t: view public.v depends on it",
		},
		{
			name:    "drop a type under CASCADE, a view on a domain over it",
			deps:    []parser.ViewDependency{dep(parser.TypeDependency, "rd", "")},
			ddl:     `CREATE DOMAIN rd AS role_t; DROP TYPE role_t CASCADE;`,
			wantErr: "cannot drop type public.rd: view public.v depends on it",
		},
		{
			name:    "drop a type under CASCADE, a view on a column of it",
			deps:    []parser.ViewDependency{dep(parser.ColumnDependency, "users", "kind")},
			ddl:     `ALTER TABLE users ADD COLUMN kind role_t; DROP TYPE role_t CASCADE;`,
			wantErr: "cannot drop column kind of table public.users: view public.v depends on it",
		},
		{
			name:    "drop a type under CASCADE, a view on a partition's column of it",
			deps:    []parser.ViewDependency{dep(parser.ColumnDependency, "events_2024", "kind")},
			ddl:     `ALTER TABLE events ADD COLUMN kind role_t; DROP TYPE role_t CASCADE;`,
			wantErr: "cannot drop column kind of table public.events_2024: view public.v depends on it",
		},
		{
			name:    "drop a type under CASCADE, a view on an attribute of it",
			deps:    []parser.ViewDependency{dep(parser.AttributeDependency, "comp", "r")},
			ddl:     `CREATE TYPE comp AS (r role_t, n int); DROP TYPE role_t CASCADE;`,
			wantErr: "cannot drop attribute r of type public.comp: view public.v depends on it",
		},
		{
			name: "drop a type under CASCADE no view depends on",
			deps: []parser.ViewDependency{dep(parser.ColumnDependency, "users", "role")},
			ddl:  `ALTER TABLE users ADD COLUMN kind role_t; DROP TYPE role_t CASCADE;`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New("")
			p.Seed(&parser.Schema{
				Tables: []parser.Table{
					{Schema: "public", Name: "users", Columns: []parser.Column{{Name: "id", Type: "integer", PrimaryKey: true}, {Name: "role", Type: "text"}, {Name: "email", Type: "text"}}},
					{Schema: "public", Name: "events", Columns: []parser.Column{{Name: "id", Type: "integer"}, {Name: "x", Type: "integer"}}},
				},
				Enums:            []parser.Enum{{Schema: "public", Name: "role_t", Values: []string{"a"}}},
				Partitions:       []parser.Partition{{Schema: "public", Name: "events_2024", ParentSchema: "public", ParentName: "events"}},
				ViewDependencies: tt.deps,
			})
			err := p.Parse("test.sql", []byte(tt.ddl))
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Parse(%q) after Seed error: %v", tt.ddl, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Parse(%q) after Seed error = %v, want containing %q", tt.ddl, err, tt.wantErr)
			}
		})
	}
}

// TestDropTypeCascadeCatalogView pins that DROP TYPE … CASCADE is refused,
// and changes nothing, when a database view has a column of the type: under
// source: both the database's view stands (PRD §6.1).
func TestDropTypeCascadeCatalogView(t *testing.T) {
	p := postgres.New("")
	p.Seed(&parser.Schema{
		Tables: []parser.Table{{Schema: "public", Name: "users", Columns: []parser.Column{{Name: "id", Type: "integer"}, {Name: "kind", Type: "role_t"}}}},
		Views:  []parser.View{{Schema: "public", Name: "vu", Columns: []parser.Column{{Name: "r", Type: "role_t"}}}},
		Enums:  []parser.Enum{{Schema: "public", Name: "role_t", Values: []string{"a"}}},
	})
	err := p.Parse("test.sql", []byte(`DROP TYPE role_t CASCADE;`))
	const want = "drop type: cannot drop type public.role_t: column public.vu.r of view uses it"
	if err == nil || !strings.HasSuffix(err.Error(), want) {
		t.Fatalf("Parse() error = %v, want it to end %q", err, want)
	}
	if got := len(p.Schema().Tables[0].Columns); got != 2 {
		t.Errorf("users has %d columns after the refused drop, want 2", got)
	}
	if got := len(p.Schema().Enums); got != 1 {
		t.Errorf("schema has %d enums after the refused drop, want 1", got)
	}
}
