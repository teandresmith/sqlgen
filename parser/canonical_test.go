package parser_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/mysql"
	"github.com/teandresmith/sqlgen/parser/postgres"
	"github.com/teandresmith/sqlgen/parser/sqlite"
)

// columnKeys is the part of a column CanonicalizeConstraints keeps in step
// with the table's constraints.
type columnKeys struct {
	Name        string
	PrimaryKey  bool
	Unique      bool
	FKReference *parser.FKReference
}

func TestCanonicalizeConstraints(t *testing.T) {
	ref := func(table, schema, col string) *parser.FKReference {
		return &parser.FKReference{Table: table, Schema: schema, Column: col}
	}
	tests := []struct {
		name        string
		dialect     string
		ddl         string
		table       string
		constraints []parser.Constraint
		columns     []columnKeys
	}{
		// PostgreSQL
		{
			name:    "postgres inline primary key",
			dialect: "postgres", table: "t",
			ddl:         `CREATE TABLE t (id int PRIMARY KEY)`,
			constraints: []parser.Constraint{{Name: "t_pkey", Type: parser.PrimaryKey, Columns: []string{"id"}}},
			columns:     []columnKeys{{Name: "id", PrimaryKey: true}},
		},
		{
			name:    "postgres table-level primary key",
			dialect: "postgres", table: "t",
			ddl:         `CREATE TABLE t (id int, PRIMARY KEY (id))`,
			constraints: []parser.Constraint{{Name: "t_pkey", Type: parser.PrimaryKey, Columns: []string{"id"}}},
			columns:     []columnKeys{{Name: "id", PrimaryKey: true}},
		},
		{
			name:    "postgres composite primary key",
			dialect: "postgres", table: "t",
			ddl:         `CREATE TABLE t (a int, b int, PRIMARY KEY (b, a))`,
			constraints: []parser.Constraint{{Name: "t_pkey", Type: parser.PrimaryKey, Columns: []string{"b", "a"}}},
			columns:     []columnKeys{{Name: "a", PrimaryKey: true}, {Name: "b", PrimaryKey: true}},
		},
		{
			name:    "postgres inline and table-level unique",
			dialect: "postgres", table: "t",
			ddl: `CREATE TABLE t (a int UNIQUE, b int, CONSTRAINT named_u UNIQUE (b))`,
			constraints: []parser.Constraint{
				{Name: "named_u", Type: parser.Unique, Columns: []string{"b"}, Method: "btree"},
				{Name: "t_a_key", Type: parser.Unique, Columns: []string{"a"}, Method: "btree"},
			},
			columns: []columnKeys{{Name: "a", Unique: true}, {Name: "b", Unique: true}},
		},
		{
			name:    "postgres inline references and table-level foreign key",
			dialect: "postgres", table: "t",
			ddl: `CREATE TABLE p (id int PRIMARY KEY);
				CREATE TABLE t (a int REFERENCES p(id), b int, FOREIGN KEY (b) REFERENCES p(id))`,
			constraints: []parser.Constraint{
				{Name: "t_a_fkey", Type: parser.ForeignKey, Columns: []string{"a"}, ReferenceTable: "p", ReferenceSchema: "public", ReferenceColumns: []string{"id"}},
				{Name: "t_b_fkey", Type: parser.ForeignKey, Columns: []string{"b"}, ReferenceTable: "p", ReferenceSchema: "public", ReferenceColumns: []string{"id"}},
			},
			columns: []columnKeys{{Name: "a", FKReference: ref("p", "public", "id")}, {Name: "b", FKReference: ref("p", "public", "id")}},
		},
		{
			name:    "postgres foreign keys on one column: the first by name gives the reference",
			dialect: "postgres", table: "t",
			ddl: `CREATE TABLE p (id int PRIMARY KEY, x int UNIQUE);
				CREATE TABLE t (a int REFERENCES p(x), CONSTRAINT aa FOREIGN KEY (a) REFERENCES p(id))`,
			constraints: []parser.Constraint{
				{Name: "aa", Type: parser.ForeignKey, Columns: []string{"a"}, ReferenceTable: "p", ReferenceSchema: "public", ReferenceColumns: []string{"id"}},
				{Name: "t_a_fkey", Type: parser.ForeignKey, Columns: []string{"a"}, ReferenceTable: "p", ReferenceSchema: "public", ReferenceColumns: []string{"x"}},
			},
			columns: []columnKeys{{Name: "a", FKReference: ref("p", "public", "id")}},
		},
		{
			name:    "postgres column and table check",
			dialect: "postgres", table: "t",
			ddl: `CREATE TABLE t (a int CHECK (a > 0), b int, CHECK (a < b))`,
			constraints: []parser.Constraint{
				{Name: "t_a_check", Type: parser.Check, Columns: []string{"a"}, CheckExpression: "a > 0"},
				{Name: "t_check", Type: parser.Check, Columns: []string{"a", "b"}, CheckExpression: "a < b"},
			},
			columns: []columnKeys{{Name: "a"}, {Name: "b"}},
		},
		{
			name:    "postgres unique repeating the primary key merges into it",
			dialect: "postgres", table: "t",
			ddl:         `CREATE TABLE t (id int PRIMARY KEY, CONSTRAINT bu UNIQUE (id))`,
			constraints: []parser.Constraint{{Name: "bu", Type: parser.PrimaryKey, Columns: []string{"id"}}},
			columns:     []columnKeys{{Name: "id", PrimaryKey: true}},
		},
		{
			name:    "postgres inline and table-level unique on one column merge",
			dialect: "postgres", table: "t",
			ddl:         `CREATE TABLE t (a int UNIQUE, UNIQUE (a))`,
			constraints: []parser.Constraint{{Name: "t_a_key", Type: parser.Unique, Columns: []string{"a"}, Method: "btree"}},
			columns:     []columnKeys{{Name: "a", Unique: true}},
		},
		{
			name:    "postgres unique added later is a second constraint",
			dialect: "postgres", table: "t",
			ddl: `CREATE TABLE t (a int UNIQUE);
				ALTER TABLE t ADD UNIQUE (a)`,
			constraints: []parser.Constraint{
				{Name: "t_a_key", Type: parser.Unique, Columns: []string{"a"}, Method: "btree"},
				{Name: "t_a_key1", Type: parser.Unique, Columns: []string{"a"}, Method: "btree"},
			},
			columns: []columnKeys{{Name: "a", Unique: true}},
		},
		// MySQL
		{
			name:    "mysql inline primary key",
			dialect: "mysql", table: "t",
			ddl:         `CREATE TABLE t (id int PRIMARY KEY)`,
			constraints: []parser.Constraint{{Name: "PRIMARY", Type: parser.PrimaryKey, Columns: []string{"id"}}},
			columns:     []columnKeys{{Name: "id", PrimaryKey: true}},
		},
		{
			name:    "mysql table-level primary key is PRIMARY whatever its declared name",
			dialect: "mysql", table: "t",
			ddl:         `CREATE TABLE t (id int, CONSTRAINT pk PRIMARY KEY (id))`,
			constraints: []parser.Constraint{{Name: "PRIMARY", Type: parser.PrimaryKey, Columns: []string{"id"}}},
			columns:     []columnKeys{{Name: "id", PrimaryKey: true}},
		},
		{
			name:    "mysql composite primary key",
			dialect: "mysql", table: "t",
			ddl:         `CREATE TABLE t (a int, b int, PRIMARY KEY (b, a))`,
			constraints: []parser.Constraint{{Name: "PRIMARY", Type: parser.PrimaryKey, Columns: []string{"b", "a"}}},
			columns:     []columnKeys{{Name: "a", PrimaryKey: true}, {Name: "b", PrimaryKey: true}},
		},
		{
			name:    "mysql inline and table-level unique",
			dialect: "mysql", table: "t",
			ddl: `CREATE TABLE t (a int UNIQUE, b int, UNIQUE KEY (B))`,
			constraints: []parser.Constraint{
				{Name: "a", Type: parser.Unique, Columns: []string{"a"}, Method: "btree"},
				{Name: "b", Type: parser.Unique, Columns: []string{"b"}, Method: "btree"},
			},
			columns: []columnKeys{{Name: "a", Unique: true}, {Name: "b", Unique: true}},
		},
		{
			name:    "mysql table-level foreign keys and the indexes built for them",
			dialect: "mysql", table: "t",
			ddl: `CREATE TABLE p (id int PRIMARY KEY);
				CREATE TABLE t (a int, b int, FOREIGN KEY (a) REFERENCES p(id), CONSTRAINT fk_b FOREIGN KEY (b) REFERENCES p(id))`,
			constraints: []parser.Constraint{
				{Name: "fk_b", Type: parser.ForeignKey, Columns: []string{"b"}, ReferenceTable: "p", ReferenceColumns: []string{"id"}},
				{Name: "t_ibfk_1", Type: parser.ForeignKey, Columns: []string{"a"}, ReferenceTable: "p", ReferenceColumns: []string{"id"}},
				{Name: "a", Type: parser.Index, Columns: []string{"a"}, Method: "btree"},
				{Name: "fk_b", Type: parser.Index, Columns: []string{"b"}, Method: "btree"},
			},
			columns: []columnKeys{{Name: "a", FKReference: ref("p", "", "id")}, {Name: "b", FKReference: ref("p", "", "id")}},
		},
		{
			name:    "mysql inline references",
			dialect: "mysql", table: "t",
			ddl: `CREATE TABLE p (id int PRIMARY KEY);
				CREATE TABLE t (a int REFERENCES p(id))`,
			constraints: []parser.Constraint{
				{Name: "t_ibfk_1", Type: parser.ForeignKey, Columns: []string{"a"}, ReferenceTable: "p", ReferenceColumns: []string{"id"}},
				{Name: "a", Type: parser.Index, Columns: []string{"a"}, Method: "btree"},
			},
			columns: []columnKeys{{Name: "a", FKReference: ref("p", "", "id")}},
		},
		{
			name:    "mysql foreign keys on one column: the first by name gives the reference",
			dialect: "mysql", table: "t",
			ddl: `CREATE TABLE p (id int PRIMARY KEY, x int UNIQUE);
				CREATE TABLE t (a int, CONSTRAINT zz FOREIGN KEY (a) REFERENCES p(x), CONSTRAINT aa FOREIGN KEY (a) REFERENCES p(id))`,
			constraints: []parser.Constraint{
				{Name: "aa", Type: parser.ForeignKey, Columns: []string{"a"}, ReferenceTable: "p", ReferenceColumns: []string{"id"}},
				{Name: "zz", Type: parser.ForeignKey, Columns: []string{"a"}, ReferenceTable: "p", ReferenceColumns: []string{"x"}},
				{Name: "aa", Type: parser.Index, Columns: []string{"a"}, Method: "btree"},
			},
			columns: []columnKeys{{Name: "a", FKReference: ref("p", "", "id")}},
		},
		{
			name:    "mysql column and table check",
			dialect: "mysql", table: "t",
			ddl: `CREATE TABLE t (a int, b int CHECK (B > 0), CONSTRAINT named_ck CHECK (a < b), CHECK (a <> 5))`,
			constraints: []parser.Constraint{
				{Name: "named_ck", Type: parser.Check, Columns: []string{"a", "b"}, CheckExpression: "a < b"},
				{Name: "t_chk_1", Type: parser.Check, Columns: []string{"b"}, CheckExpression: "B > 0"},
				{Name: "t_chk_2", Type: parser.Check, Columns: []string{"a"}, CheckExpression: "a != 5"},
			},
			columns: []columnKeys{{Name: "a"}, {Name: "b"}},
		},
		{
			name:    "mysql unique repeating the primary key is kept",
			dialect: "mysql", table: "t",
			ddl: `CREATE TABLE t (id int PRIMARY KEY, UNIQUE KEY (id))`,
			constraints: []parser.Constraint{
				{Name: "PRIMARY", Type: parser.PrimaryKey, Columns: []string{"id"}},
				{Name: "id", Type: parser.Unique, Columns: []string{"id"}, Method: "btree"},
			},
			columns: []columnKeys{{Name: "id", PrimaryKey: true, Unique: true}},
		},
		{
			name:    "mysql inline and table-level unique on one column are both kept",
			dialect: "mysql", table: "t",
			ddl: `CREATE TABLE t (a int UNIQUE, UNIQUE KEY (a))`,
			constraints: []parser.Constraint{
				{Name: "a", Type: parser.Unique, Columns: []string{"a"}, Method: "btree"},
				{Name: "a_2", Type: parser.Unique, Columns: []string{"a"}, Method: "btree"},
			},
			columns: []columnKeys{{Name: "a", Unique: true}},
		},
		// SQLite
		{
			name:    "sqlite inline primary key",
			dialect: "sqlite", table: "t",
			ddl:         `CREATE TABLE t (id INTEGER PRIMARY KEY)`,
			constraints: []parser.Constraint{{Type: parser.PrimaryKey, Columns: []string{"id"}}},
			columns:     []columnKeys{{Name: "id", PrimaryKey: true}},
		},
		{
			name:    "sqlite table-level primary key keeps its declared name",
			dialect: "sqlite", table: "t",
			ddl:         `CREATE TABLE t (id TEXT, CONSTRAINT t_pk PRIMARY KEY (id))`,
			constraints: []parser.Constraint{{Name: "t_pk", Type: parser.PrimaryKey, Columns: []string{"id"}}},
			columns:     []columnKeys{{Name: "id", PrimaryKey: true}},
		},
		{
			name:    "sqlite composite primary key",
			dialect: "sqlite", table: "t",
			ddl:         `CREATE TABLE t (a INTEGER, b INTEGER, PRIMARY KEY (b, a))`,
			constraints: []parser.Constraint{{Type: parser.PrimaryKey, Columns: []string{"b", "a"}}},
			columns:     []columnKeys{{Name: "a", PrimaryKey: true}, {Name: "b", PrimaryKey: true}},
		},
		{
			name:    "sqlite inline and table-level unique",
			dialect: "sqlite", table: "t",
			ddl: `CREATE TABLE t (a INTEGER CONSTRAINT a_uq UNIQUE, b INTEGER, UNIQUE (B))`,
			constraints: []parser.Constraint{
				{Type: parser.Unique, Columns: []string{"b"}},
				{Name: "a_uq", Type: parser.Unique, Columns: []string{"a"}},
			},
			columns: []columnKeys{{Name: "a", Unique: true}, {Name: "b", Unique: true}},
		},
		{
			name:    "sqlite inline references and table-level foreign key",
			dialect: "sqlite", table: "t",
			ddl: `CREATE TABLE p (id INTEGER PRIMARY KEY);
				CREATE TABLE t (a INTEGER REFERENCES p(id), b INTEGER, FOREIGN KEY (b) REFERENCES p(id))`,
			constraints: []parser.Constraint{
				{Type: parser.ForeignKey, Columns: []string{"a"}, ReferenceTable: "p", ReferenceColumns: []string{"id"}},
				{Type: parser.ForeignKey, Columns: []string{"b"}, ReferenceTable: "p", ReferenceColumns: []string{"id"}},
			},
			columns: []columnKeys{{Name: "a", FKReference: ref("p", "", "id")}, {Name: "b", FKReference: ref("p", "", "id")}},
		},
		{
			name:    "sqlite foreign keys on one column: the first declared gives the reference",
			dialect: "sqlite", table: "t",
			ddl: `CREATE TABLE p (id INTEGER PRIMARY KEY, x INTEGER UNIQUE);
				CREATE TABLE t (a INTEGER REFERENCES p(x) REFERENCES p(id))`,
			constraints: []parser.Constraint{
				{Type: parser.ForeignKey, Columns: []string{"a"}, ReferenceTable: "p", ReferenceColumns: []string{"x"}},
				{Type: parser.ForeignKey, Columns: []string{"a"}, ReferenceTable: "p", ReferenceColumns: []string{"id"}},
			},
			columns: []columnKeys{{Name: "a", FKReference: ref("p", "", "x")}},
		},
		{
			name:    "sqlite column and table check",
			dialect: "sqlite", table: "t",
			ddl: `CREATE TABLE t (a INTEGER CHECK (a > 0), b INTEGER, CONSTRAINT ab CHECK (A < b))`,
			constraints: []parser.Constraint{
				{Type: parser.Check, Columns: []string{"a"}, CheckExpression: `"a" > 0`},
				{Name: "ab", Type: parser.Check, Columns: []string{"a", "b"}, CheckExpression: `"A" < "b"`},
			},
			columns: []columnKeys{{Name: "a"}, {Name: "b"}},
		},
		{
			name:    "sqlite unique repeating an indexed primary key is dropped",
			dialect: "sqlite", table: "t",
			ddl:         `CREATE TABLE t (id TEXT PRIMARY KEY UNIQUE)`,
			constraints: []parser.Constraint{{Type: parser.PrimaryKey, Columns: []string{"id"}}},
			columns:     []columnKeys{{Name: "id", PrimaryKey: true}},
		},
		{
			name:    "sqlite unique on a rowid alias is kept",
			dialect: "sqlite", table: "t",
			ddl: `CREATE TABLE t (id INTEGER PRIMARY KEY UNIQUE)`,
			constraints: []parser.Constraint{
				{Type: parser.PrimaryKey, Columns: []string{"id"}},
				{Type: parser.Unique, Columns: []string{"id"}},
			},
			columns: []columnKeys{{Name: "id", PrimaryKey: true, Unique: true}},
		},
		{
			name:    "sqlite inline and table-level unique on one column are one",
			dialect: "sqlite", table: "t",
			ddl:         `CREATE TABLE t (a INTEGER UNIQUE, UNIQUE (a))`,
			constraints: []parser.Constraint{{Type: parser.Unique, Columns: []string{"a"}}},
			columns:     []columnKeys{{Name: "a", Unique: true}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newDialectParser(t, tt.dialect)
			if err := p.Parse("schema.sql", []byte(tt.ddl)); err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			table := tableNamed(t, p.Schema(), tt.table)
			if diff := cmp.Diff(tt.constraints, sortedConstraints(table.Constraints)); diff != "" {
				t.Errorf("%s constraints mismatch (-want +got):\n%s", tt.dialect, diff)
			}
			if diff := cmp.Diff(tt.columns, keysOf(table)); diff != "" {
				t.Errorf("%s column flags mismatch (-want +got):\n%s", tt.dialect, diff)
			}
		})
	}
}

// TestCanonicalizeConstraints_FromFlags covers a table whose keys exist only as
// column flags, as an introspector can build one.
func TestCanonicalizeConstraints_FromFlags(t *testing.T) {
	table := parser.Table{
		Name: "t",
		Columns: []parser.Column{
			{Name: "id", PrimaryKey: true},
			{Name: "email", Unique: true},
			{Name: "org_id", FKReference: &parser.FKReference{Table: "orgs", Column: "id"}},
			{Name: "tag", FKReference: &parser.FKReference{Table: "tags", Synthetic: true}},
		},
		Constraints: []parser.Constraint{
			{Name: "ck", Type: parser.Check, CheckExpression: "(`EMAIL` <> _utf8mb4'id')"},
			{Name: "ix", Type: parser.Index, Columns: []string{"ORG_ID"}},
		},
	}
	parser.CanonicalizeConstraints(&table, "mysql")

	want := []parser.Constraint{
		{Type: parser.PrimaryKey, Columns: []string{"id"}},
		{Type: parser.ForeignKey, Columns: []string{"org_id"}, ReferenceTable: "orgs", ReferenceColumns: []string{"id"}},
		{Type: parser.Unique, Columns: []string{"email"}, Method: "btree"},
		{Name: "ck", Type: parser.Check, Columns: []string{"email"}, CheckExpression: "(`EMAIL` <> _utf8mb4'id')"},
		{Name: "ix", Type: parser.Index, Columns: []string{"org_id"}, Method: "btree"},
	}
	if diff := cmp.Diff(want, sortedConstraints(table.Constraints)); diff != "" {
		t.Errorf("CanonicalizeConstraints() constraints mismatch (-want +got):\n%s", diff)
	}
}

func TestCheckColumns(t *testing.T) {
	cols := []parser.Column{{Name: "id"}, {Name: "c"}, {Name: "createdAt"}, {Name: "price"}, {Name: "length"}}
	tests := []struct {
		dialect string
		expr    string
		want    []string
	}{
		{dialect: "postgres", expr: "length(c::text) > 0", want: []string{"c"}},
		{dialect: "postgres", expr: "price > id", want: []string{"id", "price"}},
		{dialect: "postgres", expr: `"createdAt" IS NOT NULL`, want: []string{"createdAt"}},
		{dialect: "postgres", expr: "createdAt IS NOT NULL", want: nil},
		{dialect: "postgres", expr: "PRICE > 0", want: []string{"price"}},
		{dialect: "postgres", expr: "c <> 'price'", want: []string{"c"}},
		{dialect: "postgres", expr: "1 = 1", want: nil},
		{dialect: "postgres", expr: "(price)::numeric < 1e5", want: []string{"price"}},
		{dialect: "mysql", expr: "(`Price` >= 0)", want: []string{"price"}},
		{dialect: "mysql", expr: "c <> _utf8mb4'it\\'s price'", want: []string{"c"}},
		{dialect: "sqlite", expr: "[createdat] > 0 AND LENGTH(c) < 9", want: []string{"c", "createdAt"}},
	}
	for _, tt := range tests {
		if diff := cmp.Diff(tt.want, parser.CheckColumns(tt.expr, cols, tt.dialect)); diff != "" {
			t.Errorf("CheckColumns(%q, %s) mismatch (-want +got):\n%s", tt.expr, tt.dialect, diff)
		}
	}
}

// TestCheckColumns_NonASCIICase checks that a CHECK names the columns its
// dialect matches: SQLite folds ASCII case only, so "é" and "É" are two
// columns, while MySQL folds Unicode case.
func TestCheckColumns_NonASCIICase(t *testing.T) {
	cols := []parser.Column{{Name: "é"}, {Name: "É"}}
	tests := []struct {
		dialect string
		expr    string
		want    []string
	}{
		{dialect: "sqlite", expr: `"é" > 0`, want: []string{"é"}},
		{dialect: "mysql", expr: "`é` > 0", want: []string{"é", "É"}},
		{dialect: "postgres", expr: `"é" > 0`, want: []string{"é"}},
	}
	for _, tt := range tests {
		if diff := cmp.Diff(tt.want, parser.CheckColumns(tt.expr, cols, tt.dialect)); diff != "" {
			t.Errorf("CheckColumns(%q, %s) mismatch (-want +got):\n%s", tt.expr, tt.dialect, diff)
		}
	}
}

func newDialectParser(t *testing.T, dialect string) parser.Parser {
	t.Helper()
	switch dialect {
	case "postgres":
		return postgres.New("")
	case "mysql":
		p, err := mysql.New()
		if err != nil {
			t.Fatalf("mysql.New() error: %v", err)
		}
		return p
	case "sqlite":
		return sqlite.New()
	}
	t.Fatalf("unknown dialect %q", dialect)
	return nil
}

func tableNamed(t *testing.T, s *parser.Schema, name string) *parser.Table {
	t.Helper()
	for i := range s.Tables {
		if s.Tables[i].Name == name {
			return &s.Tables[i]
		}
	}
	t.Fatalf("table %q not found", name)
	return nil
}

// sortedConstraints orders constraints by type, then name, then columns.
func sortedConstraints(cs []parser.Constraint) []parser.Constraint {
	out := slices.Clone(cs)
	slices.SortFunc(out, func(a, b parser.Constraint) int {
		if a.Type != b.Type {
			return int(a.Type) - int(b.Type)
		}
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return slices.Compare(a.Columns, b.Columns)
	})
	return out
}

func keysOf(table *parser.Table) []columnKeys {
	out := make([]columnKeys, 0, len(table.Columns))
	for _, col := range table.Columns {
		out = append(out, columnKeys{Name: col.Name, PrimaryKey: col.PrimaryKey, Unique: col.Unique, FKReference: col.FKReference})
	}
	return out
}
