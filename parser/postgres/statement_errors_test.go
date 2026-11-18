package postgres_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser/postgres"
)

// pgwParseErr parses sql with the default schema and returns the error.
func pgwParseErr(sql string) error {
	return postgres.New("").Parse("test.sql", []byte(sql))
}

// A VIRTUAL generated column is outside the PostgreSQL 17 grammar the file
// parser ships, so the file is a parse error, whatever the column's place.
func TestGeneratedVirtualColumnIsAParseError(t *testing.T) {
	tests := []struct {
		name string
		sql  string
	}{
		{name: "in CREATE TABLE", sql: `CREATE TABLE t (a int, b int GENERATED ALWAYS AS (a * 2) VIRTUAL);`},
		{name: "added by ALTER TABLE", sql: `CREATE TABLE t (a int); ALTER TABLE t ADD COLUMN b int GENERATED ALWAYS AS (a * 2) VIRTUAL;`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pgwParseErr(tt.sql)
			if err == nil || !strings.Contains(err.Error(), `syntax error at or near "VIRTUAL"`) {
				t.Errorf("Parse(%q) error = %v, want a syntax error at VIRTUAL", tt.sql, err)
			}
		})
	}
	// The STORED form of the same column parses, so the error is VIRTUAL's.
	if err := pgwParseErr(`CREATE TABLE t (a int, b int GENERATED ALWAYS AS (a * 2) STORED);`); err != nil {
		t.Errorf("Parse() of a STORED generated column error: %v", err)
	}
}

// Every CREATE TABLE whose columns the file parser does not read says how to
// get the table modelled: write its columns out, or read it from the
// database.
func TestCreateTableUnreadColumnsAdvice(t *testing.T) {
	const base = `CREATE TABLE s (id int PRIMARY KEY);
CREATE TYPE comp AS (a int, b text);
`
	const advice = "write the columns out, or create the table in the database and read it from there (input.source: database or both)"
	tests := []struct {
		name string
		sql  string
	}{
		{name: "like", sql: `CREATE TABLE r (LIKE s);`},
		{name: "inherits", sql: `CREATE TABLE r (x int) INHERITS (s);`},
		{name: "typed table", sql: `CREATE TABLE r OF comp;`},
		{name: "create table as", sql: `CREATE TABLE r AS SELECT id FROM s;`},
		{name: "temporary create table as", sql: `CREATE TEMPORARY TABLE r AS SELECT id FROM s;`},
		{name: "select into", sql: `SELECT id INTO r FROM s;`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pgwParseErr(base + tt.sql)
			if err == nil || !strings.Contains(err.Error(), advice) {
				t.Errorf("Parse(%q) error = %v, want it to advise %q", tt.sql, err, advice)
			}
		})
	}
}

// A DROP INDEX of an index on a partition or materialized view, which the
// parser skips with the relation, changes nothing in the model: the
// partitioned table keeps its own indexes and their UNIQUE flags.
func TestDropIndexOnSkippedRelation(t *testing.T) {
	const base = `CREATE TABLE p (k int, v int) PARTITION BY RANGE (k);
CREATE TABLE p1 PARTITION OF p FOR VALUES FROM (0) TO (10);
CREATE MATERIALIZED VIEW mv AS SELECT k FROM p;
CREATE UNIQUE INDEX p_k_uq ON p (k);
CREATE INDEX p_v_ix ON p (v);
CREATE INDEX p1_k_ix ON p1 (k);
CREATE UNIQUE INDEX mv_k_uq ON mv (k);
`
	tests := []struct {
		name string
		drop string
	}{
		{name: "index on a partition", drop: `DROP INDEX p1_k_ix;`},
		{name: "index on a materialized view", drop: `DROP INDEX mv_k_uq;`},
		{name: "both in one statement", drop: `DROP INDEX p1_k_ix, mv_k_uq;`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New("")
			if err := p.Parse("test.sql", []byte(base+tt.drop)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			tables := p.Schema().Tables
			if len(tables) != 1 {
				t.Fatalf("Parse() tables = %d, want only p", len(tables))
			}
			var got []string
			for _, c := range tables[0].Constraints {
				got = append(got, c.Type.String()+" "+c.Name+" "+strings.Join(c.Columns, ","))
			}
			for _, col := range tables[0].Columns {
				if col.Unique {
					got = append(got, "unique column "+col.Name)
				}
			}
			want := []string{"UNIQUE p_k_uq k", "INDEX p_v_ix v", "unique column k"}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("table p mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// DROP TYPE removes the composite type it names, and only that one: a
// same-named composite in another schema and another composite stay.
func TestDropTypeRemovesCompositeType(t *testing.T) {
	p := postgres.New("")
	err := p.Parse("test.sql", []byte(`
		CREATE SCHEMA app;
		CREATE TYPE addr AS (street text, city text);
		CREATE TYPE app.addr AS (zip text);
		CREATE TYPE point2 AS (x int, y int);
		DROP TYPE addr;
	`))
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	got := make([]string, 0, len(p.Schema().CompositeTypes))
	for _, ct := range p.Schema().CompositeTypes {
		got = append(got, ct.Schema+"."+ct.Name)
	}
	slices.Sort(got)
	if diff := cmp.Diff([]string{"app.addr", "public.point2"}, got); diff != "" {
		t.Errorf("composite types mismatch (-want +got):\n%s", diff)
	}
}

// DROP DOMAIN refuses an enum, as PostgreSQL does, and leaves the enum in
// place.
func TestDropDomainNamingAnEnum(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantErr string
	}{
		{name: "unqualified enum", sql: `CREATE TYPE mood AS ENUM ('a'); DROP DOMAIN mood;`, wantErr: "drop domain: public.mood is not a domain"},
		{name: "qualified enum", sql: `CREATE SCHEMA app; CREATE TYPE app.mood AS ENUM ('a'); DROP DOMAIN app.mood;`, wantErr: "drop domain: app.mood is not a domain"},
		{name: "enum listed after a domain", sql: `CREATE DOMAIN d AS int; CREATE TYPE mood AS ENUM ('a'); DROP DOMAIN d, mood;`, wantErr: "drop domain: public.mood is not a domain"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pgwParseErr(tt.sql)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Parse(%q) error = %v, want containing %q", tt.sql, err, tt.wantErr)
			}
		})
	}
	// The enum the refused statement names is still there.
	p := postgres.New("")
	if err := p.Parse("test.sql", []byte(`CREATE TYPE mood AS ENUM ('a');`)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if err := p.Parse("drop.sql", []byte(`DROP DOMAIN mood;`)); err == nil {
		t.Fatal("Parse() of DROP DOMAIN on an enum succeeded, want an error")
	}
	if got := p.Schema().Enums; len(got) != 1 || got[0].Name != "mood" {
		t.Errorf("enums after the refused DROP DOMAIN = %+v, want mood", got)
	}
}
