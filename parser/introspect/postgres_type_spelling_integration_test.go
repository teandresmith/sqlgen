package introspect_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/introspect"
	"github.com/teandresmith/sqlgen/parser/postgres"
)

// TestPostgresIntrospect_BareBuiltinBesideSameNamedType pins the spelling of
// a built-in type written bare (text) where another schema declares a type
// by its name (an enum pgw_app.text), for tables and views, under files and
// database, with input.schema public.
func TestPostgresIntrospect_BareBuiltinBesideSameNamedType(t *testing.T) {
	requireDatabases(t)
	const reset = "DROP SCHEMA IF EXISTS pgw_app CASCADE; DROP SCHEMA IF EXISTS pgw_oth CASCADE; DROP TABLE IF EXISTS public.pgw_src CASCADE"
	tables := `
CREATE SCHEMA pgw_app;
CREATE SCHEMA pgw_oth;
CREATE TYPE pgw_app.text AS ENUM ('x');
CREATE TABLE pgw_src (id int PRIMARY KEY, t text);
CREATE TABLE pgw_app.own (id int PRIMARY KEY, t text)`
	views := []string{
		"CREATE VIEW pgw_app.v_default AS SELECT id, t FROM public.pgw_src",
		"CREATE VIEW pgw_app.v_own AS SELECT id, t FROM pgw_app.own",
		"CREATE VIEW pgw_oth.v_other AS SELECT id, t FROM pgw_app.own",
	}
	applyParityFixture(t, "pgx", pgConnStr, reset, tables+";"+strings.Join(views, ";"))

	files := parseStatements(t, postgres.New("public"), tables)
	for _, ddl := range views {
		parsed, err := postgres.ParseViewSQL([]byte(ddl), "public")
		if err != nil {
			t.Fatalf("ParseViewSQL(%q) error: %v", ddl, err)
		}
		view, err := parser.BuildView(parsed, ddl, &parser.ViewAnnotations{}, files, postgres.SearchPath("public"))
		if err != nil {
			t.Fatalf("BuildView(%q) error: %v", ddl, err)
		}
		files.Views = append(files.Views, *view)
	}

	intro := introspect.NewPostgresIntrospector()
	defer intro.Close() //nolint:errcheck // test cleanup
	database := &parser.Schema{}
	opts := introspect.IntrospectionOptions{Schemas: []string{"public", "pgw_app", "pgw_oth"}, DefaultSchema: "public"}
	if err := intro.Introspect(context.Background(), pgConnStr, database, opts); err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}

	relations := []string{"public.pgw_src", "pgw_app.own", "pgw_app.v_default", "pgw_app.v_own", "pgw_oth.v_other"}
	tests := []struct {
		name   string
		schema *parser.Schema
		want   map[string]string
	}{
		{
			name:   "files",
			schema: files,
			want: map[string]string{
				"public.pgw_src.t":    "text",
				"pgw_app.own.t":       "public.text",
				"pgw_app.v_default.t": "public.text",
				"pgw_app.v_own.t":     "public.text",
				"pgw_oth.v_other.t":   "text",
			},
		},
		{
			name:   "database",
			schema: database,
			want: map[string]string{
				"public.pgw_src.t":    "text",
				"pgw_app.own.t":       "text",
				"pgw_app.v_default.t": "public.text",
				"pgw_app.v_own.t":     "text",
				"pgw_oth.v_other.t":   "pgw_app.text",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := make(map[string]string)
			add := func(schema, name string, cols []parser.Column) {
				for _, c := range cols {
					if c.Name == "t" {
						got[schema+"."+name+".t"] = c.Type
					}
				}
			}
			for _, tbl := range tt.schema.Tables {
				add(tbl.Schema, tbl.Name, tbl.Columns)
			}
			for _, v := range tt.schema.Views {
				add(v.Schema, v.Name, v.Columns)
			}
			mine := make(map[string]string)
			for _, rel := range relations {
				if typ, ok := got[rel+".t"]; ok {
					mine[rel+".t"] = typ
				}
			}
			if diff := cmp.Diff(tt.want, mine); diff != "" {
				t.Errorf("%s column types mismatch (-want +got):\n%s", tt.name, diff)
			}
		})
	}
}

// TestPostgresIntrospect_ForeignKeyMatchSimple pins that introspection
// records MATCH SIMPLE from pg_constraint.confmatchtype for a key declared
// MATCH SIMPLE or with no MATCH clause, beside a MATCH FULL key on the same
// table.
func TestPostgresIntrospect_ForeignKeyMatchSimple(t *testing.T) {
	requireDatabases(t)
	const reset = "DROP SCHEMA IF EXISTS pgw_fk CASCADE"
	ddl := `
CREATE SCHEMA pgw_fk;
CREATE TABLE pgw_fk.p (a int, b int, PRIMARY KEY (a, b));
CREATE TABLE pgw_fk.c (
	x int, y int, z int,
	CONSTRAINT c_full FOREIGN KEY (x, y) REFERENCES pgw_fk.p (a, b) MATCH FULL,
	CONSTRAINT c_simple FOREIGN KEY (y, z) REFERENCES pgw_fk.p (a, b) MATCH SIMPLE,
	CONSTRAINT c_default FOREIGN KEY (z, x) REFERENCES pgw_fk.p (a, b)
)`
	applyParityFixture(t, "pgx", pgConnStr, reset, ddl)
	s := introspectPG(t, "", "pgw_fk")
	got := make(map[string]string)
	for _, tbl := range s.Tables {
		if tbl.Schema != "pgw_fk" || tbl.Name != "c" {
			continue
		}
		for _, c := range tbl.Constraints {
			if c.Type == parser.ForeignKey {
				got[c.Name] = c.Match.String()
			}
		}
	}
	want := map[string]string{"c_full": "FULL", "c_simple": "SIMPLE", "c_default": "SIMPLE"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("introspected FOREIGN KEY match types mismatch (-want +got):\n%s", diff)
	}
}
