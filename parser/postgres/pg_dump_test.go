package postgres_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser/postgres"
)

// TestPgDumpRestrictLines pins that the `\restrict` and `\unrestrict` lines
// pg_dump writes since PostgreSQL 17.6 are left out, that a backslash inside
// a string literal or a dollar-quoted body is left alone, and that any other
// psql meta-command is an error naming it and its line.
func TestPgDumpRestrictLines(t *testing.T) {
	tests := []struct {
		name        string
		sql         string
		wantDefault string // the default of t.body, when the file parses
		wantErr     string
	}{
		{
			name: "restrict and unrestrict around the dump",
			sql: "--\n-- PostgreSQL database dump\n--\n\n\\restrict emU4lCDX1VuMswxwAIguQGn7uadov5AkOQ\n\n" +
				"CREATE TABLE public.t (id integer NOT NULL, body text DEFAULT 'x'::text);\n\n" +
				"\\unrestrict emU4lCDX1VuMswxwAIguQGn7uadov5AkOQ\n",
			wantDefault: "'x'::text",
		},
		{
			name:        "CRLF line endings and leading space",
			sql:         "  \\restrict k\r\nCREATE TABLE t (id int, body text DEFAULT 'x');\r\n\\unrestrict k",
			wantDefault: "'x'",
		},
		{
			name:        "backslash line inside a string literal",
			sql:         "CREATE TABLE t (id int, body text DEFAULT '\n\\restrict x\n');",
			wantDefault: "E'\n\\\\restrict x\n'",
		},
		{
			name:        "backslash line inside a dollar-quoted body",
			sql:         "CREATE TABLE t (id int, body text DEFAULT $$\n\\restrict y\n$$);",
			wantDefault: "E'\n\\\\restrict y\n'",
		},
		{
			name:    "another meta-command",
			sql:     "\\restrict k\nCREATE TABLE t (id int);\n\\connect other\nCREATE TABLE u (id int);",
			wantErr: `psql meta-command \connect at line 3`,
		},
		{
			name:    "a bare backslash",
			sql:     "CREATE TABLE t (id int);\n\\\n",
			wantErr: `psql meta-command \ at line 2`,
		},
		{
			name:    "mid-line backslash stays a syntax error",
			sql:     "CREATE TABLE t (id int); \\restrict k",
			wantErr: `syntax error at or near "\"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New("")
			err := p.Parse("dump.sql", []byte(tt.sql))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Parse() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			tables := p.Schema().Tables
			if len(tables) != 1 || tables[0].Name != "t" || len(tables[0].Columns) != 2 {
				t.Fatalf("tables = %+v, want t (id, body)", tables)
			}
			if got := tables[0].Columns[1].Default; got != tt.wantDefault {
				t.Errorf("t.body default = %q, want %q", got, tt.wantDefault)
			}
		})
	}
}

// TestForeignTable pins that a foreign table is a relation the parser skips
// with a warning, as the introspector reads only base tables: it takes its
// name, COMMENT ON and ALTER TABLE actions on it are skipped, an index on it
// is an error, as PostgreSQL 17 refuses one, and DROP FOREIGN TABLE, RENAME
// TO and SET SCHEMA follow it.
func TestForeignTable(t *testing.T) {
	const base = `CREATE TABLE t (id int PRIMARY KEY);
CREATE FOREIGN TABLE public.ft (
    a integer NOT NULL,
    b text
)
SERVER s
OPTIONS (
    table_name 'remote'
);
`
	tests := []struct {
		name       string
		sql        string
		wantTables []string
		wantErr    string
	}{
		{name: "comment on column", sql: `COMMENT ON COLUMN public.ft.a IS 'x';`, wantTables: []string{"t"}},
		{name: "comment on foreign table", sql: `COMMENT ON FOREIGN TABLE public.ft IS 'x';`, wantTables: []string{"t"}},
		{name: "alter table add column", sql: `ALTER TABLE ft ADD COLUMN c int;`, wantTables: []string{"t"}},
		{name: "create index", sql: `CREATE INDEX ON ft (a);`, wantErr: "create index: table public.ft not found"},
		{name: "foreign table of its name", sql: `CREATE FOREIGN TABLE ft (a int) SERVER s;`, wantErr: "relation public.ft already exists"},
		{name: "foreign table of a table's name", sql: `CREATE FOREIGN TABLE t (a int) SERVER s;`, wantErr: "relation public.t already exists"},
		{name: "if not exists", sql: `CREATE FOREIGN TABLE IF NOT EXISTS t (a int) SERVER s;`, wantTables: []string{"t"}},
		{
			name:       "drop, then a relation of its name",
			sql:        `DROP FOREIGN TABLE ft; CREATE FOREIGN TABLE ft (a int) SERVER s; DROP FOREIGN TABLE ft; CREATE TABLE ft (id int); CREATE INDEX ON ft (id);`,
			wantTables: []string{"t", "ft"},
		},
		{
			name:       "rename, then a table of its old name",
			sql:        `ALTER FOREIGN TABLE ft RENAME TO ft2; COMMENT ON COLUMN ft2.a IS 'x'; CREATE FOREIGN TABLE ft (a int) SERVER s;`,
			wantTables: []string{"t"},
		},
		{
			name:    "rename onto a table",
			sql:     `ALTER FOREIGN TABLE ft RENAME TO t;`,
			wantErr: "relation public.t already exists",
		},
		{
			name:       "set schema",
			sql:        `CREATE SCHEMA o; ALTER FOREIGN TABLE ft SET SCHEMA o; COMMENT ON COLUMN o.ft.a IS 'x'; CREATE FOREIGN TABLE ft (a int) SERVER s;`,
			wantTables: []string{"t"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := postgres.New("")
			err := p.Parse("test.sql", []byte(base+tt.sql))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Parse() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			var got []string
			for _, tbl := range p.Schema().Tables {
				got = append(got, tbl.Name)
			}
			if diff := cmp.Diff(tt.wantTables, got); diff != "" {
				t.Errorf("tables mismatch (-want +got):\n%s", diff)
			}
			if w := p.Schema().Warnings; len(w) == 0 || w[0] != "CREATE FOREIGN TABLE ft skipped in DDL file — foreign tables are not modelled" {
				t.Errorf("warnings = %q, want the foreign table's first", w)
			}
		})
	}
}
