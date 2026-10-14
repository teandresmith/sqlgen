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
			// format_type writes these bare under the default search_path
			// (PostgreSQL 17), so the introspector does too.
			name:  "an undeclared type qualified with the default schema is written bare",
			ddl:   `CREATE TABLE r (id int); CREATE TABLE app.t (h public.hstore, ha public.hstore[], rr public.r, c ext.citext)`,
			table: "app.t",
			want:  map[string]string{"h": "hstore", "ha": "hstore[]", "rr": "r", "c": "ext.citext"},
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

// TestTypeNamesStripDefaultSchema checks that an undeclared type qualified
// with the parser's default schema is written bare wherever a type is written,
// and that only the default schema is stripped.
func TestTypeNamesStripDefaultSchema(t *testing.T) {
	ddl := `
CREATE TABLE t (c app.citext, h public.hstore);
CREATE DOMAIN d AS app.citext;
CREATE TYPE comp AS (c app.citext[]);
`
	p := postgres.New("app")
	if err := p.Parse("test.sql", []byte(ddl)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	s := p.Schema()
	got := map[string]string{
		"t.c":    s.Tables[0].Columns[0].Type,
		"t.h":    s.Tables[0].Columns[1].Type,
		"d":      s.DomainTypes[0].BaseType,
		"comp.c": s.CompositeTypes[0].Attributes[0].Type,
	}
	want := map[string]string{"t.c": "citext", "t.h": "public.hstore", "d": "citext", "comp.c": "citext[]"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("type names mismatch (-want +got):\n%s", diff)
	}
}

// TestTypeNamesShadowedDefaultSchema pins the spelling of a default-schema
// type the DDL does not declare in a schema that declares a type by the same
// name: default-schema qualified while the shadowing type exists, whatever
// the statement order, and bare again once it is renamed, dropped or the
// table leaves the schema, as the introspector writes it.
func TestTypeNamesShadowedDefaultSchema(t *testing.T) {
	tests := []struct {
		name string
		ddl  string
		want map[string]string
	}{
		{
			name: "shadowed by a type declared before or after",
			ddl: `CREATE TABLE r (x int); CREATE TABLE app.t (rr public.r, rb r, ra r[]);
				CREATE TYPE app.r AS ENUM ('a'); CREATE TABLE app.u (mine app.r, rr public.r);
				CREATE DOMAIN app.dr AS public.r; CREATE TYPE app.comp AS (rr r);`,
			want: map[string]string{
				"public.r.x": "integer", "app.t.rr": "public.r", "app.t.rb": "public.r", "app.t.ra": "public.r[]",
				"app.u.mine": "r", "app.u.rr": "public.r", "domain app.dr": "public.r", "attr app.comp.rr": "public.r",
			},
		},
		{
			name: "renamed away",
			ddl: `CREATE TABLE r (x int); CREATE TYPE app.r AS ENUM ('a');
				CREATE TABLE app.t (rr public.r, mine app.r); ALTER TYPE app.r RENAME TO y;`,
			want: map[string]string{"public.r.x": "integer", "app.t.rr": "r", "app.t.mine": "y"},
		},
		{
			name: "dropped",
			ddl:  `CREATE TABLE r (x int); CREATE TYPE app.r AS ENUM ('a'); CREATE TABLE app.t (rr public.r); DROP TYPE app.r;`,
			want: map[string]string{"public.r.x": "integer", "app.t.rr": "r"},
		},
		{
			name: "table moved in and out",
			ddl: `CREATE TABLE r (x int); CREATE TYPE app.r AS ENUM ('a'); CREATE TABLE m (rr r);
				ALTER TABLE m SET SCHEMA app; CREATE TABLE app.n (rr r); ALTER TABLE app.n SET SCHEMA public;`,
			want: map[string]string{"public.r.x": "integer", "app.m.rr": "public.r", "public.n.rr": "r"},
		},
		{
			name: "type modifiers shadowed by a type declared before, then dropped",
			ddl: `CREATE DOMAIN app.vector AS int; CREATE TABLE app.t (v vector(3), va vector(3)[]);
				CREATE TABLE app.u (v public.vector(3));
				CREATE TABLE app.w (v vector(3), va vector(3)[]); DROP DOMAIN app.vector;`,
			want: map[string]string{
				"app.t.v": "vector(3)", "app.t.va": "vector(3)[]", "app.u.v": "vector(3)",
				"app.w.v": "vector(3)", "app.w.va": "vector(3)[]",
			},
		},
		{
			name: "type modifiers shadowed by a type declared after",
			ddl:  `CREATE TABLE app.t (v vector(3), va vector(3)[]); CREATE DOMAIN app.vector AS int;`,
			want: map[string]string{
				"app.t.v": "public.vector(3)", "app.t.va": "public.vector(3)[]", "domain app.vector": "integer",
			},
		},
		{
			name: "grammar types a type declared after or a move shadows",
			ddl: `CREATE TABLE x (created timestamptz, e timetz); CREATE TABLE app.y (e timestamptz(3), f int4);
				CREATE TABLE vb (b varbit); CREATE TABLE app.vb (b varbit(2));
				CREATE TABLE app.t (a varchar(3), b int, c timestamp(3) with time zone, d varchar(3)[], e time(2) with time zone,
					p bit varying(4));
				CREATE TABLE m (a varchar(3), c timestamp with time zone, e timestamptz(3), p bit varying(4));
				CREATE DOMAIN app.varchar AS int; CREATE DOMAIN app.integer AS int; CREATE DOMAIN app.timestamptz AS int;
				CREATE DOMAIN app.timetz AS int; CREATE DOMAIN app.varbit AS int; ALTER TABLE m SET SCHEMA app;`,
			want: map[string]string{
				"public.x.created": "timestamptz", "public.x.e": "timetz", "app.y.e": "timestamptz(3)", "app.y.f": "integer",
				"public.vb.b": "varbit", "app.vb.b": "varbit(2)",
				"app.t.a": "varchar(3)", "app.t.b": "integer", "app.t.c": "timestamptz(3)", "app.t.d": "varchar(3)[]",
				"app.t.e": "timetz(2)", "app.t.p": "varbit(4)", "app.m.a": "varchar(3)", "app.m.c": "timestamptz",
				"app.m.e": "timestamptz(3)", "app.m.p": "varbit(4)",
				"domain app.varchar": "integer", "domain app.integer": "integer", "domain app.timestamptz": "integer",
				"domain app.timetz": "integer", "domain app.varbit": "integer",
			},
		},
		{
			name: "built-in types a type declared before shadows",
			ddl: `CREATE DOMAIN app.integer AS int; CREATE DOMAIN app.timestamptz AS int;
				CREATE TABLE app.t (a int4, b timestamptz(3), c timestamp with time zone);`,
			want: map[string]string{
				"app.t.a": "integer", "app.t.b": "timestamptz(3)", "app.t.c": "timestamptz",
				"domain app.integer": "integer", "domain app.timestamptz": "integer",
			},
		},
		{
			name: "default schema and other-schema qualifiers",
			ddl:  `CREATE TYPE app.citext AS ENUM ('a'); CREATE TABLE t (c citext); CREATE TABLE app.t (c ext.citext);`,
			want: map[string]string{"public.t.c": "citext", "app.t.c": "ext.citext"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New("public")
			if err := p.Parse("test.sql", []byte(tt.ddl)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			s := p.Schema()
			got := make(map[string]string)
			for _, tbl := range s.Tables {
				for _, c := range tbl.Columns {
					got[tbl.Schema+"."+tbl.Name+"."+c.Name] = c.Type
				}
			}
			for _, d := range s.DomainTypes {
				got["domain "+d.Schema+"."+d.Name] = d.BaseType
			}
			for _, ct := range s.CompositeTypes {
				for _, a := range ct.Attributes {
					got["attr "+ct.Schema+"."+ct.Name+"."+a.Name] = a.Type
				}
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("type names mismatch (-want +got):\n%s", diff)
			}
		})
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
