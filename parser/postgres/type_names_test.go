package postgres_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/postgres"
)

// typeNamesSetup declares user types in three schemas. The parser's default
// schema is public.
const typeNamesSetup = `
CREATE TYPE app.status_enum AS ENUM ('a');
CREATE TYPE other.region AS ENUM ('us');
CREATE TYPE public.mood AS ENUM ('ok');
CREATE DOMAIN app.positive_int AS integer;
CREATE TYPE app.addr AS (street text);
`

func TestTypeNamesFollowColumnSchema(t *testing.T) {
	tests := []struct {
		name  string
		ddl   string
		table string // schema.name
		want  map[string]string
	}{
		{
			name:  "own-schema types are written bare",
			ddl:   `CREATE TABLE app.t (e app.status_enum, ea app.status_enum[], d app.positive_int, c app.addr)`,
			table: "app.t",
			want:  map[string]string{"e": "status_enum", "ea": "status_enum[]", "d": "positive_int", "c": "addr"},
		},
		{
			name:  "cross-schema types stay qualified",
			ddl:   `CREATE TABLE app.t (r other.region, ra other.region[])`,
			table: "app.t",
			want:  map[string]string{"r": "other.region", "ra": "other.region[]"},
		},
		{
			name:  "an unqualified type resolves to the default schema",
			ddl:   `CREATE TABLE app.t (m mood); CREATE TABLE public.u (m mood, q public.mood)`,
			table: "app.t",
			want:  map[string]string{"m": "public.mood"},
		},
		{
			name:  "a default-schema type in a default-schema table is bare",
			ddl:   `CREATE TABLE u (m mood, q public.mood)`,
			table: "public.u",
			want:  map[string]string{"m": "mood", "q": "mood"},
		},
		{
			name:  "types the file does not declare are written as given",
			ddl:   `CREATE TABLE app.t (x app.citext, y citext, z pg_catalog.int4, w text)`,
			table: "app.t",
			want:  map[string]string{"x": "app.citext", "y": "citext", "z": "integer", "w": "text"},
		},
		{
			name:  "ALTER COLUMN TYPE",
			ddl:   `CREATE TABLE app.t (e text, r text); ALTER TABLE app.t ALTER COLUMN e TYPE app.status_enum, ALTER COLUMN r TYPE other.region`,
			table: "app.t",
			want:  map[string]string{"e": "status_enum", "r": "other.region"},
		},
		{
			name:  "ADD COLUMN",
			ddl:   `CREATE TABLE app.t (id int); ALTER TABLE app.t ADD COLUMN e app.status_enum`,
			table: "app.t",
			want:  map[string]string{"id": "integer", "e": "status_enum"},
		},
		{
			name:  "SET SCHEMA rewrites the types for the new schema",
			ddl:   `CREATE TABLE app.t (e app.status_enum, ea app.status_enum[], r other.region, x text); ALTER TABLE app.t SET SCHEMA other`,
			table: "other.t",
			want:  map[string]string{"e": "app.status_enum", "ea": "app.status_enum[]", "r": "region", "x": "text"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New("public")
			if err := p.Parse("test.sql", []byte(typeNamesSetup+tt.ddl)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			table := findTable(t, p.Schema(), tt.table)
			got := make(map[string]string)
			for _, c := range table.Columns {
				got[c.Name] = c.Type
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("%s column types mismatch (-want +got):\n%s", tt.table, diff)
			}
		})
	}
}

func TestTypeNamesInCompositesAndDomains(t *testing.T) {
	ddl := typeNamesSetup + `
CREATE TYPE other.wrap AS (r other.region, s app.status_enum, n integer);
CREATE DOMAIN other.region_d AS other.region;
CREATE DOMAIN other.status_d AS app.status_enum;
`
	p := postgres.New("public")
	if err := p.Parse("test.sql", []byte(ddl)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	s := p.Schema()

	var wrap *parser.CompositeType
	for i := range s.CompositeTypes {
		if s.CompositeTypes[i].Name == "wrap" {
			wrap = &s.CompositeTypes[i]
		}
	}
	if wrap == nil {
		t.Fatal("composite type other.wrap not found")
	}
	wantAttrs := []parser.Attribute{{Name: "r", Type: "region"}, {Name: "s", Type: "app.status_enum"}, {Name: "n", Type: "integer"}}
	if diff := cmp.Diff(wantAttrs, wrap.Attributes); diff != "" {
		t.Errorf("other.wrap attributes mismatch (-want +got):\n%s", diff)
	}

	gotBase := make(map[string]string)
	for _, d := range s.DomainTypes {
		gotBase[d.Schema+"."+d.Name] = d.BaseType
	}
	wantBase := map[string]string{"app.positive_int": "integer", "other.region_d": "region", "other.status_d": "app.status_enum"}
	if diff := cmp.Diff(wantBase, gotBase); diff != "" {
		t.Errorf("domain base types mismatch (-want +got):\n%s", diff)
	}
}

// TestDomainCheckNames pins the names PostgreSQL 16 gave the same domains: an
// unnamed CHECK is <domain>_check, a name already taken by any constraint in
// the schema gets a numeric suffix, and a long name is cut to 63 bytes.
func TestDomainCheckNames(t *testing.T) {
	ddl := `
CREATE TABLE dn.t (a int, CONSTRAINT d_check CHECK (a > 0));
CREATE DOMAIN dn.d AS int CHECK (VALUE > 0);
CREATE TABLE dn2.t (a int, CONSTRAINT e_check CHECK (a > 0));
CREATE DOMAIN dn.e AS int CHECK (VALUE > 0) CHECK (VALUE < 9);
CREATE DOMAIN dn.dom_re AS varchar(20) CONSTRAINT dom_re_check CHECK (VALUE ~ '^[a-z]+$') CHECK (VALUE <> '');
CREATE DOMAIN dn.abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefghij_xy AS int CHECK (VALUE > 0);
CREATE DOMAIN dn.nn AS int NOT NULL CHECK (VALUE >= 0);
`
	p := postgres.New("public")
	if err := p.Parse("test.sql", []byte(ddl)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	type check struct{ Name, Expr string }
	got := make(map[string][]check)
	for _, d := range p.Schema().DomainTypes {
		for _, c := range d.Constraints {
			got[d.Name] = append(got[d.Name], check{c.Name, c.CheckExpression})
		}
	}
	want := map[string][]check{
		"d":      {{"d_check1", "value > 0"}},
		"e":      {{"e_check", "value > 0"}, {"e_check1", "value < 9"}},
		"dom_re": {{"dom_re_check", "value ~ '^[a-z]+$'"}, {"dom_re_check1", "value <> ''"}},
		"nn":     {{"nn_check", "value >= 0"}},
		"abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefghij_xy": {
			{"abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefg_check", "value > 0"},
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("domain constraints mismatch (-want +got):\n%s", diff)
	}
}

func TestPartitionOfSkipped(t *testing.T) {
	ddl := `
CREATE TABLE events (
	id bigint NOT NULL,
	created_at date NOT NULL,
	PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);
CREATE TABLE events_2024 PARTITION OF events FOR VALUES FROM ('2024-01-01') TO ('2025-01-01');
CREATE TABLE app.events_2025 PARTITION OF events FOR VALUES FROM ('2025-01-01') TO ('2026-01-01');
`
	p := postgres.New("public")
	if err := p.Parse("test.sql", []byte(ddl)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	s := p.Schema()
	tables := make([]string, 0, len(s.Tables))
	for _, tbl := range s.Tables {
		tables = append(tables, tbl.Schema+"."+tbl.Name)
	}
	if diff := cmp.Diff([]string{"public.events"}, tables); diff != "" {
		t.Errorf("tables mismatch (-want +got):\n%s", diff)
	}
	wantWarnings := []string{
		"CREATE TABLE events_2024 PARTITION OF events skipped in DDL file — the partitioned table is the entity",
		"CREATE TABLE events_2025 PARTITION OF events skipped in DDL file — the partitioned table is the entity",
	}
	if diff := cmp.Diff(wantWarnings, s.Warnings); diff != "" {
		t.Errorf("warnings mismatch (-want +got):\n%s", diff)
	}
}

func findTable(t *testing.T, s *parser.Schema, qualified string) *parser.Table {
	t.Helper()
	for i := range s.Tables {
		if s.Tables[i].Schema+"."+s.Tables[i].Name == qualified {
			return &s.Tables[i]
		}
	}
	t.Fatalf("table %s not found", qualified)
	return nil
}
