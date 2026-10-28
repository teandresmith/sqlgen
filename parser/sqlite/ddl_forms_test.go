package sqlite_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/sqlite"
)

// mswParse parses sql into a fresh SQLite parser and returns its schema,
// failing the test on any error.
func mswParse(t *testing.T, sql string) *parser.Schema {
	t.Helper()
	p := sqlite.New()
	if err := p.Parse("schema.sql", []byte(sql)); err != nil {
		t.Fatalf("Parse(%q) error: %v", sql, err)
	}
	return p.Schema()
}

// mswTable returns the table named name in s.
func mswTable(t *testing.T, s *parser.Schema, name string) *parser.Table {
	t.Helper()
	for i := range s.Tables {
		if s.Tables[i].Name == name {
			return &s.Tables[i]
		}
	}
	t.Fatalf("table %q not found", name)
	return nil
}

// GENERATED ALWAYS AS (expr) VIRTUAL is a generated column whose storage is
// VIRTUAL; STORED beside it stays STORED.
func TestGeneratedColumnVirtual(t *testing.T) {
	s := mswParse(t, `CREATE TABLE t (
		a INTEGER,
		v INTEGER GENERATED ALWAYS AS (a * 2) VIRTUAL,
		s INTEGER GENERATED ALWAYS AS (a + 1) STORED
	);`)
	table := mswTable(t, s, "t")
	type generated struct{ Expr, Storage string }
	got := make(map[string]generated)
	for _, c := range table.Columns {
		got[c.Name] = generated{c.GeneratedExpr, c.GeneratedStorage}
	}
	want := map[string]generated{
		"a": {},
		"v": {Expr: `"a" * 2`, Storage: "VIRTUAL"},
		"s": {Expr: `"a" + 1`, Storage: "STORED"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("generated columns mismatch (-want +got):\n%s", diff)
	}
}

// SQLite has no ALTER TABLE … MODIFY COLUMN: it is a parse error, and the
// ALTER TABLE forms SQLite has parse.
func TestAlterTableModifyColumnUnsupported(t *testing.T) {
	const base = "CREATE TABLE t (id INTEGER PRIMARY KEY, a TEXT);\n"
	tests := []struct {
		name    string
		sql     string
		wantErr string
	}{
		{name: "modify column", sql: "ALTER TABLE t MODIFY COLUMN a INTEGER;", wantErr: "found MODIFY"},
		{name: "modify without COLUMN", sql: "ALTER TABLE t MODIFY a INTEGER;", wantErr: "found MODIFY"},
		{name: "add column", sql: "ALTER TABLE t ADD COLUMN b TEXT;"},
		{name: "rename column", sql: "ALTER TABLE t RENAME COLUMN a TO b;"},
		{name: "drop column", sql: "ALTER TABLE t DROP COLUMN a;"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sqlite.New().Parse("schema.sql", []byte(base+tt.sql))
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Parse(%q) error: %v", tt.sql, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Parse(%q) error = %v, want containing %q", tt.sql, err, tt.wantErr)
			}
		})
	}
}

// SQLite accepts any MATCH clause on a FOREIGN KEY, PARTIAL included, and
// enforces MATCH SIMPLE whatever it says.
func TestForeignKeyAnyMatchAccepted(t *testing.T) {
	tests := []struct {
		name  string
		match string
	}{
		{name: "simple", match: "MATCH SIMPLE"},
		{name: "full", match: "MATCH FULL"},
		{name: "partial", match: "MATCH PARTIAL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := mswParse(t, `CREATE TABLE p (a INTEGER, b INTEGER, PRIMARY KEY (a, b));
				CREATE TABLE c (x INTEGER REFERENCES p (a) `+tt.match+`, y INTEGER, z INTEGER,
					CONSTRAINT c_yz FOREIGN KEY (y, z) REFERENCES p (a, b) `+tt.match+`);`)
			var got []string
			for _, c := range mswTable(t, s, "c").Constraints {
				if c.Type == parser.ForeignKey {
					got = append(got, c.Name+" ["+strings.Join(c.Columns, ",")+"] -> "+c.ReferenceTable+" ["+strings.Join(c.ReferenceColumns, ",")+"] "+c.Match.String())
				}
			}
			want := []string{" [x] -> p [a] SIMPLE", "c_yz [y,z] -> p [a,b] SIMPLE"}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("foreign keys mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// SQLite requires a CREATE INDEX to be named: an unnamed one is a parse error.
func TestCreateIndexNameRequired(t *testing.T) {
	const base = "CREATE TABLE t (id INTEGER PRIMARY KEY, a TEXT);\n"
	tests := []struct {
		name    string
		sql     string
		wantErr string
	}{
		{name: "unnamed index", sql: "CREATE INDEX ON t (a);", wantErr: "expected index name"},
		{name: "unnamed unique index", sql: "CREATE UNIQUE INDEX ON t (a);", wantErr: "expected index name"},
		{name: "named index", sql: "CREATE INDEX ix_a ON t (a);"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sqlite.New().Parse("schema.sql", []byte(base+tt.sql))
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Parse(%q) error: %v", tt.sql, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Parse(%q) error = %v, want containing %q", tt.sql, err, tt.wantErr)
			}
		})
	}
}

// A CREATE TABLE … AS SELECT is an error that tells the user to write the
// columns out or to read the table from the database.
func TestUnreadColumnsErrorAdvice(t *testing.T) {
	const sql = "CREATE TABLE s (id INTEGER PRIMARY KEY, name TEXT NOT NULL);\nCREATE TABLE c AS SELECT id, name FROM s;"
	err := sqlite.New().Parse("schema.sql", []byte(sql))
	if err == nil {
		t.Fatalf("Parse(%q) error = nil, want the unread-columns error", sql)
	}
	for _, want := range []string{
		"write the columns out",
		"create the table in the database and read it from there",
		"input.source: database or both",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Parse() error = %q, want it to contain %q", err, want)
		}
	}
}

// A SQLite table has no schema: a qualifier on CREATE TABLE is dropped and
// the table's name is bare.
func TestQualifiedTableNameIsBare(t *testing.T) {
	tests := []struct {
		name string
		sql  string
	}{
		{name: "public qualifier", sql: "CREATE TABLE public.products (id INTEGER PRIMARY KEY);"},
		{name: "other schema qualifier", sql: "CREATE TABLE myschema.products (id INTEGER PRIMARY KEY);"},
		{name: "quoted qualifier", sql: `CREATE TABLE "myschema"."products" (id INTEGER PRIMARY KEY);`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := mswParse(t, tt.sql)
			if len(s.Tables) != 1 {
				t.Fatalf("tables = %d, want 1", len(s.Tables))
			}
			if got := s.Tables[0]; got.Schema != "" || got.Name != "products" {
				t.Errorf("table = %q.%q, want the bare table products", got.Schema, got.Name)
			}
		})
	}
}
