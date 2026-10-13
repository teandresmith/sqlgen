package postgres_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/postgres"
)

func TestAlterEnumValues(t *testing.T) {
	sql := `
		CREATE TYPE mood AS ENUM ('ok', 'meh');
		CREATE DOMAIN dmood AS mood;
		CREATE TABLE t (
			a mood DEFAULT 'meh',
			b dmood DEFAULT 'meh',
			c mood DEFAULT 'meh'::mood,
			d mood DEFAULT 'ok',
			e mood[],
			f text DEFAULT 'meh'
		);
		ALTER TYPE mood ADD VALUE 'great' BEFORE 'ok';
		ALTER TYPE mood ADD VALUE IF NOT EXISTS 'ok';
		ALTER TYPE mood ADD VALUE IF NOT EXISTS 'ok' BEFORE 'nope';
		ALTER TYPE mood ADD VALUE 'end';
		ALTER TYPE mood ADD VALUE 'mid' AFTER 'ok';
		ALTER TYPE mood RENAME VALUE 'meh' TO 'bad';
	`
	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	// PostgreSQL 17: {great,ok,mid,bad,end}. A default that is the renamed
	// label reads back as the new label, through a domain too.
	if diff := cmp.Diff([]string{"great", "ok", "mid", "bad", "end"}, p.Schema().Enums[0].Values); diff != "" {
		t.Errorf("enum values mismatch (-want +got):\n%s", diff)
	}
	got := make(map[string]string)
	for _, c := range p.Schema().Tables[0].Columns {
		got[c.Name] = c.Default
	}
	want := map[string]string{"a": "'bad'", "b": "'bad'", "c": "'bad'::mood", "d": "'ok'", "e": "", "f": "'meh'"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("defaults mismatch (-want +got):\n%s", diff)
	}
}

func TestRenameEnumValueInExpressions(t *testing.T) {
	sql := `
		CREATE TYPE mood AS ENUM ('ok', 'meh', 'it''s');
		CREATE DOMAIN dmood AS mood CHECK (VALUE <> 'meh');
		CREATE DOMAIN dtext AS text CHECK (VALUE <> 'meh');
		CREATE TABLE t (
			m mood, d dmood, s text,
			g text GENERATED ALWAYS AS (CASE WHEN m = 'meh' THEN 'meh' ELSE s END) STORED,
			CONSTRAINT c1 CHECK (m <> 'meh'),
			CONSTRAINT c2 CHECK (m IN ('ok', 'meh')),
			CONSTRAINT c3 CHECK (s <> 'meh'),
			CONSTRAINT c4 CHECK ('meh'::mood <> m),
			CONSTRAINT c5 CHECK (s::mood <> 'meh'),
			CONSTRAINT c6 CHECK (m <> 'it''s')
		);
		CREATE INDEX pi ON t (m) WHERE m = 'meh';
		ALTER TYPE mood RENAME VALUE 'meh' TO 'bad';
		ALTER TYPE mood RENAME VALUE 'it''s' TO 'x';
	`
	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	s := p.Schema()
	got := map[string]string{"g": s.Tables[0].Columns[3].GeneratedExpr}
	for _, c := range s.Tables[0].Constraints {
		got[c.Name] = c.CheckExpression + c.Where
	}
	for _, dt := range s.DomainTypes {
		got[dt.Name] = dt.Constraints[0].CheckExpression
	}
	// PostgreSQL 17's pg_get_constraintdef / pg_get_expr: a constant of the
	// enum takes the new label; a text constant keeps the old one.
	want := map[string]string{
		"g":     "CASE WHEN m = 'bad' THEN 'meh' ELSE s END",
		"c1":    "m <> 'bad'",
		"c2":    "m IN ('ok', 'bad')",
		"c3":    "s <> 'meh'",
		"c4":    "'bad'::mood <> m",
		"c5":    "s::mood <> 'bad'",
		"c6":    "m <> 'x'",
		"pi":    "m = 'bad'",
		"dmood": "value <> 'bad'",
		"dtext": "value <> 'meh'",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("expressions mismatch (-want +got):\n%s", diff)
	}
}

func TestAlterEnumErrors(t *testing.T) {
	tests := []struct {
		name string
		stmt string
		want string
	}{
		{"label exists", "ALTER TYPE mood ADD VALUE 'ok'", `enum label "ok" already exists`},
		{"unknown neighbor", "ALTER TYPE mood ADD VALUE 'x' BEFORE 'nope'", `"nope" is not an existing enum label`},
		{"rename unknown label", "ALTER TYPE mood RENAME VALUE 'nope' TO 'y'", `"nope" is not an existing enum label`},
		{"rename onto a label", "ALTER TYPE mood RENAME VALUE 'ok' TO 'meh'", `enum label "meh" already exists`},
		{"unknown enum", "ALTER TYPE nope ADD VALUE 'x'", "alter type public.nope: unknown enum"},
		{"not an enum", "ALTER TYPE dd ADD VALUE 'x'", "alter type public.dd: unknown enum"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql := "CREATE TYPE mood AS ENUM ('ok', 'meh'); CREATE DOMAIN dd AS int; " + tt.stmt
			err := postgres.New("").Parse("test.sql", []byte(sql))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Parse() error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestRenameType(t *testing.T) {
	sql := `
		CREATE SCHEMA s2;
		CREATE TYPE mood AS ENUM ('ok');
		CREATE DOMAIN d1 AS mood;
		CREATE DOMAIN dd AS int;
		CREATE TYPE comp AS (m mood, ms mood[], d d1, x int);
		CREATE TABLE t (m mood, ms mood[], d d1, c comp, cs comp[], dd dd, x int);
		CREATE TABLE s2.t2 (m mood, ms mood[], d d1, c comp);
		CREATE TYPE s2.mood AS ENUM ('other');
		CREATE TABLE s2.t3 (m s2.mood, n mood);
		ALTER TYPE mood RENAME TO mood2;
		ALTER DOMAIN d1 RENAME TO d2;
		ALTER TYPE dd RENAME TO dd2;
		ALTER TYPE comp RENAME TO comp2;
		ALTER TYPE comp2 RENAME ATTRIBUTE x TO y;
	`
	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	s := p.Schema()
	got := map[string]string{}
	for _, tbl := range s.Tables {
		for _, c := range tbl.Columns {
			got[tbl.Schema+"."+tbl.Name+"."+c.Name] = c.Type
		}
	}
	for _, ct := range s.CompositeTypes {
		for _, a := range ct.Attributes {
			got["type "+ct.Schema+"."+ct.Name+"."+a.Name] = a.Type
		}
	}
	for _, dt := range s.DomainTypes {
		got["domain "+dt.Schema+"."+dt.Name] = dt.BaseType
	}
	for _, e := range s.Enums {
		got["enum "+e.Schema+"."+e.Name] = strings.Join(e.Values, ",")
	}
	// PostgreSQL 17's format_type, written relative to the owner's schema:
	// every use of a renamed type follows it, and s2.mood is another type.
	want := map[string]string{
		"public.t.m":           "mood2",
		"public.t.ms":          "mood2[]",
		"public.t.d":           "d2",
		"public.t.c":           "comp2",
		"public.t.cs":          "comp2[]",
		"public.t.dd":          "dd2",
		"public.t.x":           "integer",
		"s2.t2.m":              "public.mood2",
		"s2.t2.ms":             "public.mood2[]",
		"s2.t2.d":              "public.d2",
		"s2.t2.c":              "public.comp2",
		"s2.t3.m":              "mood",
		"s2.t3.n":              "public.mood2",
		"type public.comp2.m":  "mood2",
		"type public.comp2.ms": "mood2[]",
		"type public.comp2.d":  "d2",
		"type public.comp2.y":  "integer",
		"domain public.d2":     "mood2",
		"domain public.dd2":    "integer",
		"enum public.mood2":    "ok",
		"enum s2.mood":         "other",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("types mismatch (-want +got):\n%s", diff)
	}
}

// TestRenameEnumValueInSeededCheck covers the form introspection reads
// m IN ('ok', 'meh') in, under source both.
func TestRenameEnumValueInSeededCheck(t *testing.T) {
	p := postgres.New("")
	p.Seed(&parser.Schema{
		Enums: []parser.Enum{{Schema: "public", Name: "mood", Values: []string{"ok", "meh"}}},
		Tables: []parser.Table{{
			Schema: "public", Name: "t", Columns: []parser.Column{{Name: "m", Type: "mood"}},
			Constraints: []parser.Constraint{{Name: "c", Type: parser.Check, CheckExpression: "m = ANY(ARRAY['ok', 'meh'])"}},
		}},
	})
	if err := p.Parse("test.sql", []byte("ALTER TYPE mood RENAME VALUE 'meh' TO 'bad'")); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if got, want := p.Schema().Tables[0].Constraints[0].CheckExpression, "m = ANY(ARRAY['ok', 'bad'])"; got != want {
		t.Errorf("check = %q, want %q", got, want)
	}
}

func TestRenameTypeInSeededViews(t *testing.T) {
	p := postgres.New("")
	p.Seed(&parser.Schema{
		Enums: []parser.Enum{{Schema: "public", Name: "mood", Values: []string{"ok"}}},
		Views: []parser.View{{Schema: "public", Name: "v", Columns: []parser.Column{{Name: "m", Type: "mood[]"}}}},
	})
	if err := p.Parse("test.sql", []byte("ALTER TYPE mood RENAME TO mood2")); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if got := p.Schema().Views[0].Columns[0].Type; got != "mood2[]" {
		t.Errorf("view column type = %q, want mood2[]", got)
	}
}

func TestRenameTypeErrors(t *testing.T) {
	tests := []struct {
		name string
		stmt string
		want string
	}{
		{"domain rename of an enum", "ALTER DOMAIN mood RENAME TO mood3", "alter domain public.mood: unknown domain"},
		{"onto a type", "ALTER TYPE mood RENAME TO comp", "type public.comp already exists"},
		{"onto itself", "ALTER TYPE mood RENAME TO mood", "type public.mood already exists"},
		{"onto a table", "ALTER TYPE mood RENAME TO tb", "type public.tb already exists"},
		{"composite onto an index", "ALTER TYPE comp RENAME TO tb_pkey", "relation public.tb_pkey already exists"},
		{"table row type", "ALTER TYPE tb RENAME TO tb2", "alter type public.tb: unknown type"},
		{"unknown type", "ALTER TYPE nosuch RENAME TO x", "alter type public.nosuch: unknown type"},
		{"unknown attribute", "ALTER TYPE comp RENAME ATTRIBUTE z TO y", "alter type public.comp: unknown attribute z"},
		{"attribute exists", "ALTER TYPE comp RENAME ATTRIBUTE a TO b", "alter type public.comp: attribute b already exists"},
		{"attribute of an enum", "ALTER TYPE mood RENAME ATTRIBUTE a TO c", "alter type public.mood: unknown composite type"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql := "CREATE TYPE mood AS ENUM ('ok'); CREATE TYPE comp AS (a int, b int); CREATE TABLE tb (id int PRIMARY KEY); " + tt.stmt
			err := postgres.New("").Parse("test.sql", []byte(sql))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Parse() error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}
