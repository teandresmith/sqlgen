package mysql_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
)

// MODIFY COLUMN redefines the whole column, so a later file that leaves NOT
// NULL out of the new definition makes the column nullable.
func TestModifyColumnWithoutNotNullMakesNullable(t *testing.T) {
	tests := []struct {
		name         string
		modify       string
		wantType     string
		wantNullable bool
	}{
		{name: "without NOT NULL", modify: "ALTER TABLE t MODIFY COLUMN a BIGINT;", wantType: "bigint", wantNullable: true},
		{name: "with NULL", modify: "ALTER TABLE t MODIFY COLUMN a BIGINT NULL;", wantType: "bigint", wantNullable: true},
		{name: "with NOT NULL", modify: "ALTER TABLE t MODIFY COLUMN a BIGINT NOT NULL;", wantType: "bigint", wantNullable: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := mswParse(t, "CREATE TABLE t (id INT PRIMARY KEY, a INT NOT NULL);", tt.modify)
			col := parser.ColumnByName(mswTable(t, s, "t"), "a")
			if col == nil {
				t.Fatal("column a not found")
			}
			if col.Type != tt.wantType || col.Nullable != tt.wantNullable {
				t.Errorf("column a = %s nullable=%v, want %s nullable=%v", col.Type, col.Nullable, tt.wantType, tt.wantNullable)
			}
		})
	}
}

// CREATE TABLE IF NOT EXISTS of a table an earlier file created is skipped:
// the table keeps the columns it was created with.
func TestCreateTableIfNotExistsOnExistingTable(t *testing.T) {
	s := mswParse(t,
		"CREATE TABLE t (id INT PRIMARY KEY, a INT NOT NULL);",
		"CREATE TABLE IF NOT EXISTS t (z TEXT, w INT);",
	)
	if len(s.Tables) != 1 {
		t.Fatalf("tables = %d, want 1", len(s.Tables))
	}
	table := mswTable(t, s, "t")
	if diff := cmp.Diff([]string{"id", "a"}, mswColumnNames(*table)); diff != "" {
		t.Errorf("columns mismatch (-want +got):\n%s", diff)
	}
	if col := parser.ColumnByName(table, "a"); col == nil || col.Nullable {
		t.Errorf("column a = %+v, want NOT NULL as first created", col)
	}

	// Without IF NOT EXISTS the same statement is an error.
	err := newParser(t).Parse("schema.sql", []byte("CREATE TABLE t (id INT PRIMARY KEY);\nCREATE TABLE t (z TEXT);"))
	if err == nil || !strings.Contains(err.Error(), "table t already exists") {
		t.Errorf("Parse() error = %v, want table t already exists", err)
	}
}

// MySQL ignores CASCADE on DROP TABLE, so a table another table's foreign key
// references still cannot be dropped, and nothing is dropped.
func TestDropTableCascadeIgnored(t *testing.T) {
	p := newParser(t)
	if err := p.Parse("001.sql", []byte(`CREATE TABLE p (id INT PRIMARY KEY);
		CREATE TABLE c (id INT PRIMARY KEY, pid INT, CONSTRAINT c_p FOREIGN KEY (pid) REFERENCES p (id));`)); err != nil {
		t.Fatalf("Parse(001) error: %v", err)
	}
	err := p.Parse("002.sql", []byte("DROP TABLE p CASCADE;"))
	if err == nil || !strings.Contains(err.Error(), "cannot drop table p: column c.pid references it via foreign key") {
		t.Fatalf("Parse(DROP TABLE p CASCADE) error = %v, want cannot drop table p: column c.pid references it via foreign key", err)
	}
	s := p.Schema()
	names := make([]string, 0, len(s.Tables))
	for _, table := range s.Tables {
		names = append(names, table.Name)
	}
	if diff := cmp.Diff([]string{"p", "c"}, names); diff != "" {
		t.Errorf("tables after the refused drop mismatch (-want +got):\n%s", diff)
	}
	fk := findConstraint(mswTable(t, s, "c").Constraints, "c_p", parser.ForeignKey)
	if fk == nil || fk.ReferenceTable != "p" {
		t.Errorf("FOREIGN KEY c_p = %+v, want it to still reference p", fk)
	}
}

// A MySQL table has no schema: a qualifier on CREATE TABLE is dropped and the
// table's name is bare.
func TestQualifiedTableNameIsBare(t *testing.T) {
	tests := []struct {
		name string
		sql  string
	}{
		{name: "public qualifier", sql: "CREATE TABLE public.products (id INT PRIMARY KEY);"},
		{name: "other schema qualifier", sql: "CREATE TABLE myschema.products (id INT PRIMARY KEY);"},
		{name: "quoted qualifier", sql: "CREATE TABLE `myschema`.`products` (id INT PRIMARY KEY);"},
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
