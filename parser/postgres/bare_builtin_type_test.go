package postgres_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser/postgres"
)

// pgwColumnTypes parses ddl with input.schema public and returns every table
// column's type, keyed schema.table.column.
func pgwColumnTypes(t *testing.T, ddl string) map[string]string {
	t.Helper()
	p := postgres.New("public")
	if err := p.Parse("test.sql", []byte(ddl)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	got := make(map[string]string)
	for _, tbl := range p.Schema().Tables {
		for _, c := range tbl.Columns {
			got[tbl.Schema+"."+tbl.Name+"."+c.Name] = c.Type
		}
	}
	return got
}

// A built-in type the DDL names bare in a schema that declares a type of the
// same name is read as an input.schema type, so it is written
// public-qualified, whichever is declared first; outside that schema it stays
// bare.
func TestBareBuiltinTypeBesideSameNamedType(t *testing.T) {
	tests := []struct {
		name string
		ddl  string
		want map[string]string
	}{
		{
			name: "enum declared before the table",
			ddl: `CREATE TYPE app.text AS ENUM ('x');
				CREATE TABLE app.own (id int, t text, ts text[]);
				CREATE TABLE src (t text);`,
			want: map[string]string{
				"app.own.id": "integer", "app.own.t": "public.text", "app.own.ts": "public.text[]",
				"public.src.t": "text",
			},
		},
		{
			name: "enum declared after the table",
			ddl: `CREATE TABLE app.own (id int, t text);
				CREATE TABLE src (t text);
				CREATE TYPE app.text AS ENUM ('x');`,
			want: map[string]string{"app.own.id": "integer", "app.own.t": "public.text", "public.src.t": "text"},
		},
		{
			name: "the enum itself keeps its own spelling",
			ddl: `CREATE TYPE app.text AS ENUM ('x');
				CREATE TABLE app.own (mine app.text, t text);`,
			want: map[string]string{"app.own.mine": "text", "app.own.t": "public.text"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, pgwColumnTypes(t, tt.ddl)); diff != "" {
				t.Errorf("column types mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// A built-in written bare beside a same-named domain in the column's schema
// is read as that domain: DROP DOMAIN is refused because the column uses it,
// and renaming the domain retypes the column. A column of another schema is
// not the domain's.
func TestBareBuiltinBesideOwnSchemaDomain(t *testing.T) {
	t.Run("DROP DOMAIN is refused", func(t *testing.T) {
		tests := []struct {
			name string
			ddl  string
		}{
			{name: "domain declared before the table", ddl: `CREATE DOMAIN app.integer AS text; CREATE TABLE app.t (a int);`},
			{name: "domain declared after the table", ddl: `CREATE TABLE app.t (a int); CREATE DOMAIN app.integer AS text;`},
		}
		const want = "cannot drop type app.integer: column app.t.a uses it"
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := postgres.New("public").Parse("test.sql", []byte(tt.ddl+` DROP DOMAIN app.integer;`))
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Errorf("Parse() error = %v, want containing %q", err, want)
				}
			})
		}
		// With the column in another schema the domain drops.
		if err := postgres.New("public").Parse("test.sql", []byte(`CREATE DOMAIN app.integer AS text; CREATE TABLE t (a int); DROP DOMAIN app.integer;`)); err != nil {
			t.Errorf("Parse() of DROP DOMAIN with the column in public error: %v", err)
		}
	})
	t.Run("ALTER DOMAIN RENAME TO retypes the column", func(t *testing.T) {
		got := pgwColumnTypes(t, `CREATE DOMAIN app.integer AS text;
			CREATE TABLE app.t (a int, b int4, c bigint);
			CREATE TABLE u (a int);
			ALTER DOMAIN app.integer RENAME TO num;`)
		want := map[string]string{"app.t.a": "num", "app.t.b": "num", "app.t.c": "bigint", "public.u.a": "integer"}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("column types mismatch (-want +got):\n%s", diff)
		}
	})
}

// With input.schema public, an unqualified table is in public.
func TestUnqualifiedTableInPublic(t *testing.T) {
	p := postgres.New("public")
	if err := p.Parse("test.sql", []byte(`CREATE TABLE products (id int PRIMARY KEY);`)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	got := make([]string, 0, len(p.Schema().Tables))
	for _, tbl := range p.Schema().Tables {
		got = append(got, tbl.Schema+"."+tbl.Name)
	}
	if diff := cmp.Diff([]string{"public.products"}, got); diff != "" {
		t.Errorf("tables mismatch (-want +got):\n%s", diff)
	}
}
