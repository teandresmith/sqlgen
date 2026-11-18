package mysql_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
)

// mswParse parses each of files in turn into a fresh MySQL parser and returns
// its schema, failing the test on any error.
func mswParse(t *testing.T, files ...string) *parser.Schema {
	t.Helper()
	p := newParser(t)
	for i, sql := range files {
		if err := p.Parse("file.sql", []byte(sql)); err != nil {
			t.Fatalf("Parse(file %d) error: %v", i+1, err)
		}
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

// mswConstraints describes every constraint of table as
// "TYPE name [columns] method".
func mswConstraints(table *parser.Table) []string {
	out := make([]string, 0, len(table.Constraints))
	for _, c := range table.Constraints {
		out = append(out, c.Type.String()+" "+c.Name+" ["+strings.Join(c.Columns, ",")+"] "+c.Method)
	}
	return out
}

// mswUniqueColumns returns the names of table's columns flagged Unique.
func mswUniqueColumns(table *parser.Table) []string {
	var out []string
	for _, c := range table.Columns {
		if c.Unique {
			out = append(out, c.Name)
		}
	}
	return out
}

// MySQL fixes a constraint's name when it creates it: renaming a column the
// constraint covers changes its columns, never its name.
func TestConstraintNamesKeptAcrossColumnRename(t *testing.T) {
	s := mswParse(t,
		`CREATE TABLE p (id INT PRIMARY KEY);
		CREATE TABLE c (
			id INT PRIMARY KEY,
			pid INT,
			amount INT,
			qty INT,
			UNIQUE (amount),
			CHECK (qty > 0),
			FOREIGN KEY (pid) REFERENCES p (id)
		);`,
		`ALTER TABLE c RENAME COLUMN amount TO total;
		ALTER TABLE c RENAME COLUMN pid TO owner_id;`,
	)
	want := []string{
		"PRIMARY KEY PRIMARY [id] ",
		"UNIQUE amount [total] btree",
		"INDEX pid [owner_id] btree",
		"CHECK c_chk_1 [qty] ",
		"FOREIGN KEY c_ibfk_1 [owner_id] ",
	}
	if diff := cmp.Diff(want, mswConstraints(mswTable(t, s, "c"))); diff != "" {
		t.Errorf("constraints after the column renames mismatch (-want +got):\n%s", diff)
	}
}

// A UNIQUE declared without USING is a btree, however it is declared; one
// declared USING HASH keeps its method.
func TestUniqueMethodDefaultsToBtree(t *testing.T) {
	s := mswParse(t, `CREATE TABLE t (
			a INT,
			b INT UNIQUE,
			UNIQUE KEY ua (a),
			UNIQUE KEY uh (a, b) USING HASH
		);
		ALTER TABLE t ADD UNIQUE KEY uba (b, a);
		CREATE UNIQUE INDEX ci ON t (a);`)
	want := []string{
		"UNIQUE b [b] btree",
		"UNIQUE ua [a] btree",
		"UNIQUE uh [a,b] hash",
		"UNIQUE uba [b,a] btree",
		"UNIQUE ci [a] btree",
	}
	if diff := cmp.Diff(want, mswConstraints(mswTable(t, s, "t"))); diff != "" {
		t.Errorf("constraints mismatch (-want +got):\n%s", diff)
	}
}

// A CREATE TABLE that takes its columns from another table or a query is an
// error that tells the user to write the columns out or to read the table
// from the database.
func TestUnreadColumnsErrorAdvice(t *testing.T) {
	const base = "CREATE TABLE s (id INT PRIMARY KEY, name VARCHAR(50) NOT NULL);\n"
	tests := []struct {
		name string
		sql  string
	}{
		{name: "like", sql: "CREATE TABLE r LIKE s;"},
		{name: "as select", sql: "CREATE TABLE r AS SELECT id FROM s;"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := newParser(t).Parse("schema.sql", []byte(base+tt.sql))
			if err == nil {
				t.Fatalf("Parse(%q) error = nil, want the unread-columns error", tt.sql)
			}
			for _, want := range []string{
				"write the columns out",
				"create the table in the database and read it from there",
				"input.source: database or both",
			} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Parse(%q) error = %q, want it to contain %q", tt.sql, err, want)
				}
			}
		})
	}
}

// A CREATE INDEX needs a table an earlier statement created. A view is no
// such table, whether a DDL file declares it (and the parser skips it) or the
// database defines it.
func TestCreateIndexNeedsItsTable(t *testing.T) {
	tests := []struct {
		name    string
		seed    *parser.Schema
		sql     string
		wantErr string
	}{
		{
			name:    "no statement created the table",
			sql:     "CREATE TABLE t (a INT);\nCREATE INDEX i ON missing (a);",
			wantErr: "table missing not found",
		},
		{
			name:    "the table is created later",
			sql:     "CREATE INDEX i ON t (a);\nCREATE TABLE t (a INT);",
			wantErr: "table t not found",
		},
		{
			name:    "a view in the same file",
			sql:     "CREATE TABLE t (a INT);\nCREATE VIEW v AS SELECT a FROM t;\nCREATE INDEX i ON v (a);",
			wantErr: "table v not found",
		},
		{
			name: "a view the database defines",
			seed: &parser.Schema{
				Tables: []parser.Table{{Name: "t", Columns: []parser.Column{{Name: "a", Type: "int", Nullable: true}}}},
				Views:  []parser.View{{Name: "v", Columns: []parser.Column{{Name: "a", Type: "int", Nullable: true}}}},
			},
			sql:     "CREATE INDEX i ON v (a);",
			wantErr: "table v not found",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newParser(t)
			if tt.seed != nil {
				p.Seed(tt.seed)
			}
			err := p.Parse("schema.sql", []byte(tt.sql))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Parse(%q) error = %v, want containing %q", tt.sql, err, tt.wantErr)
			}
		})
	}
}

// DROP INDEX removes the index a CREATE INDEX added, leaving the table's other
// indexes, and re-derives the column UNIQUE flag a dropped unique index set.
func TestDropIndexOfCreatedIndex(t *testing.T) {
	const setup = `CREATE TABLE t (id INT PRIMARY KEY, a INT, b INT, c INT, UNIQUE KEY uc (c));
		CREATE UNIQUE INDEX ua ON t (a);
		CREATE INDEX ib ON t (b);
		CREATE UNIQUE INDEX uc2 ON t (c);`
	tests := []struct {
		name            string
		drop            string
		wantConstraints []string
		wantUnique      []string
	}{
		{
			name: "nothing dropped",
			drop: "",
			wantConstraints: []string{
				"PRIMARY KEY PRIMARY [id] ", "UNIQUE uc [c] btree", "UNIQUE ua [a] btree", "INDEX ib [b] btree", "UNIQUE uc2 [c] btree",
			},
			wantUnique: []string{"a", "c"},
		},
		{
			name: "a plain index",
			drop: "DROP INDEX ib ON t;",
			wantConstraints: []string{
				"PRIMARY KEY PRIMARY [id] ", "UNIQUE uc [c] btree", "UNIQUE ua [a] btree", "UNIQUE uc2 [c] btree",
			},
			wantUnique: []string{"a", "c"},
		},
		{
			name: "the only unique index on its column clears the flag",
			drop: "DROP INDEX ua ON t;",
			wantConstraints: []string{
				"PRIMARY KEY PRIMARY [id] ", "UNIQUE uc [c] btree", "INDEX ib [b] btree", "UNIQUE uc2 [c] btree",
			},
			wantUnique: []string{"c"},
		},
		{
			name: "a unique index beside another unique key keeps the flag",
			drop: "DROP INDEX uc2 ON t;",
			wantConstraints: []string{
				"PRIMARY KEY PRIMARY [id] ", "UNIQUE uc [c] btree", "UNIQUE ua [a] btree", "INDEX ib [b] btree",
			},
			wantUnique: []string{"a", "c"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := mswParse(t, setup, tt.drop)
			table := mswTable(t, s, "t")
			if diff := cmp.Diff(tt.wantConstraints, mswConstraints(table)); diff != "" {
				t.Errorf("constraints mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantUnique, mswUniqueColumns(table)); diff != "" {
				t.Errorf("unique columns mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// A DROP INDEX of a name no index holds changes nothing in the model.
func TestDropIndexOfUnknownName(t *testing.T) {
	const setup = `CREATE TABLE t (id INT PRIMARY KEY, a INT UNIQUE, b INT);
		CREATE INDEX ib ON t (b);`
	before := mswParse(t, setup)
	after := mswParse(t, setup, "DROP INDEX nothing ON t;")
	if diff := cmp.Diff(before, after); diff != "" {
		t.Errorf("schema after dropping an unknown index mismatch (-before +after):\n%s", diff)
	}
	want := []string{"PRIMARY KEY PRIMARY [id] ", "UNIQUE a [a] btree", "INDEX ib [b] btree"}
	if diff := cmp.Diff(want, mswConstraints(mswTable(t, after, "t"))); diff != "" {
		t.Errorf("constraints mismatch (-want +got):\n%s", diff)
	}
}
