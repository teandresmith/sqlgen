package sqlite_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/sqlite"
)

// TestCreateIndexOnUnknownTable pins that CREATE INDEX on a table no earlier
// statement created is an error, as it is in SQLite ("no such table:
// main.nope", "views may not be indexed", "virtual tables may not be
// indexed").
func TestCreateIndexOnUnknownTable(t *testing.T) {
	const base = `CREATE TABLE t (id INTEGER PRIMARY KEY);
CREATE VIEW v AS SELECT id FROM t;
CREATE VIRTUAL TABLE f USING fts5(body);
`
	tests := []struct {
		name    string
		ddl     string
		wantErr string
	}{
		{name: "missing table", ddl: `CREATE INDEX ix ON nope (id);`, wantErr: "create index ix: table nope not found"},
		{name: "table created later", ddl: `CREATE INDEX ix ON u (id); CREATE TABLE u (id INTEGER);`, wantErr: "create index ix: table u not found"},
		{name: "view", ddl: `CREATE INDEX ix ON v (id);`, wantErr: "create index ix: table v not found"},
		{name: "virtual table", ddl: `CREATE INDEX ix ON f (body);`, wantErr: "create index ix: table f not found"},
		{name: "dropped table", ddl: `DROP TABLE t; CREATE INDEX ix ON t (id);`, wantErr: "create index ix: table t not found"},
		{name: "table in another case", ddl: `CREATE INDEX ix ON T (id);`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := sqlite.New()
			err := p.Parse("test.sql", []byte(base+tt.ddl))
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Parse(%q) error: %v", tt.ddl, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Parse(%q) error = %v, want containing %q", tt.ddl, err, tt.wantErr)
			}
		})
	}
}

// TestDropIndex pins DROP INDEX: a CREATE INDEX index leaves its table and the
// column flags it set are re-derived, matched by name as SQLite matches it
// (ASCII case-insensitive). A UNIQUE constraint's index cannot be dropped by
// name ("no such index: u"), and an index the parser never recorded leaves
// the schema as it is.
func TestDropIndex(t *testing.T) {
	const base = `CREATE TABLE t (id INTEGER PRIMARY KEY, a TEXT, b TEXT, c TEXT, CONSTRAINT u UNIQUE (c));
CREATE UNIQUE INDEX ix_a ON t (a);
CREATE INDEX ix_b ON t (b);
CREATE UNIQUE INDEX ix_b_p ON t (b) WHERE id > 0;
CREATE INDEX ix_expr ON t (lower(a));
`
	pk := parser.Constraint{Type: parser.PrimaryKey, Columns: []string{"id"}}
	u := parser.Constraint{Name: "u", Type: parser.Unique, Columns: []string{"c"}}
	a := parser.Constraint{Name: "ix_a", Type: parser.Unique, Columns: []string{"a"}, Method: "btree"}
	b := parser.Constraint{Name: "ix_b", Type: parser.Index, Columns: []string{"b"}, Method: "btree"}
	bPartial := parser.Constraint{Name: "ix_b_p", Type: parser.Unique, Columns: []string{"b"}, Method: "btree", Where: `"id" > 0`}
	tests := []struct {
		name       string
		ddl        string
		want       []parser.Constraint
		wantUnique []string
	}{
		{name: "unique index", ddl: `DROP INDEX ix_a;`, want: []parser.Constraint{pk, u, b, bPartial}, wantUnique: []string{"c"}},
		{name: "index", ddl: `DROP INDEX ix_b;`, want: []parser.Constraint{pk, u, a, bPartial}, wantUnique: []string{"a", "c"}},
		{name: "name in another case", ddl: `DROP INDEX IX_A;`, want: []parser.Constraint{pk, u, b, bPartial}, wantUnique: []string{"c"}},
		{name: "schema qualified", ddl: `DROP INDEX IF EXISTS main.ix_a;`, want: []parser.Constraint{pk, u, b, bPartial}, wantUnique: []string{"c"}},
		{name: "unique constraint name", ddl: `DROP INDEX u;`, want: []parser.Constraint{pk, u, a, b, bPartial}, wantUnique: []string{"a", "c"}},
		{name: "expression index", ddl: `DROP INDEX ix_expr;`, want: []parser.Constraint{pk, u, a, b, bPartial}, wantUnique: []string{"a", "c"}},
		{name: "unknown", ddl: `DROP INDEX nope;`, want: []parser.Constraint{pk, u, a, b, bPartial}, wantUnique: []string{"a", "c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := sqlite.New()
			if err := p.Parse("test.sql", []byte(base+tt.ddl)); err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.ddl, err)
			}
			table := p.Schema().Tables[0]
			if diff := cmp.Diff(tt.want, table.Constraints); diff != "" {
				t.Errorf("Parse(%q): constraints mismatch (-want +got):\n%s", tt.ddl, diff)
			}
			var unique []string
			for _, col := range table.Columns {
				if col.Unique {
					unique = append(unique, col.Name)
				}
			}
			if diff := cmp.Diff(tt.wantUnique, unique); diff != "" {
				t.Errorf("Parse(%q): unique columns mismatch (-want +got):\n%s", tt.ddl, diff)
			}
		})
	}
}
