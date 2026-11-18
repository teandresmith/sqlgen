package mysql_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
)

// The vitess grammar rejects some DDL that MySQL 8.0 accepts. Each such form
// is a parse error naming the token the grammar stopped at, while the
// equivalent form it does read parses.
func TestUnreadableDDLIsAParseError(t *testing.T) {
	const parent = "CREATE TABLE p (id INT PRIMARY KEY);\n"
	tests := []struct {
		name     string
		sql      string
		wantNear string // the token the syntax error names
		readable string // the same table written in a form the grammar reads
	}{
		{name: "type synonym DEC", sql: "CREATE TABLE t (a DEC(10,2));", wantNear: "DEC", readable: "CREATE TABLE t (a DECIMAL(10,2));"},
		{name: "type synonym FIXED", sql: "CREATE TABLE t (a FIXED(10,2));", wantNear: "FIXED", readable: "CREATE TABLE t (a DECIMAL(10,2));"},
		{name: "type synonym DOUBLE PRECISION", sql: "CREATE TABLE t (a DOUBLE PRECISION);", wantNear: "PRECISION", readable: "CREATE TABLE t (a DOUBLE);"},
		{name: "type synonym NCHAR", sql: "CREATE TABLE t (a NCHAR(3));", wantNear: "NCHAR", readable: "CREATE TABLE t (a CHAR(3));"},
		{name: "type synonym NATIONAL CHAR", sql: "CREATE TABLE t (a NATIONAL CHAR(3));", wantNear: "NATIONAL", readable: "CREATE TABLE t (a CHAR(3));"},
		{name: "type synonym NATIONAL VARCHAR", sql: "CREATE TABLE t (a NATIONAL VARCHAR(3));", wantNear: "NATIONAL", readable: "CREATE TABLE t (a VARCHAR(3));"},
		{name: "type synonym NVARCHAR", sql: "CREATE TABLE t (a NVARCHAR(3));", wantNear: "NVARCHAR", readable: "CREATE TABLE t (a VARCHAR(3));"},
		{name: "type synonym CHARACTER", sql: "CREATE TABLE t (a CHARACTER(3));", wantNear: "CHARACTER", readable: "CREATE TABLE t (a CHAR(3));"},
		{name: "type synonym CHARACTER VARYING", sql: "CREATE TABLE t (a CHARACTER VARYING(3));", wantNear: "CHARACTER", readable: "CREATE TABLE t (a VARCHAR(3));"},
		{name: "type synonym INT1", sql: "CREATE TABLE t (a INT1);", wantNear: "INT1", readable: "CREATE TABLE t (a TINYINT);"},
		{name: "type synonym INT2", sql: "CREATE TABLE t (a INT2);", wantNear: "INT2", readable: "CREATE TABLE t (a SMALLINT);"},
		{name: "type synonym INT3", sql: "CREATE TABLE t (a INT3);", wantNear: "INT3", readable: "CREATE TABLE t (a MEDIUMINT);"},
		{name: "type synonym INT4", sql: "CREATE TABLE t (a INT4);", wantNear: "INT4", readable: "CREATE TABLE t (a INT);"},
		{name: "type synonym INT8", sql: "CREATE TABLE t (a INT8);", wantNear: "INT8", readable: "CREATE TABLE t (a BIGINT);"},
		{name: "type synonym MIDDLEINT", sql: "CREATE TABLE t (a MIDDLEINT);", wantNear: "MIDDLEINT", readable: "CREATE TABLE t (a MEDIUMINT);"},
		{name: "type synonym LONG", sql: "CREATE TABLE t (a LONG);", wantNear: "LONG", readable: "CREATE TABLE t (a MEDIUMTEXT);"},
		{name: "type synonym LONG VARCHAR", sql: "CREATE TABLE t (a LONG VARCHAR);", wantNear: "LONG", readable: "CREATE TABLE t (a MEDIUMTEXT);"},
		{name: "type synonym LONG VARBINARY", sql: "CREATE TABLE t (a LONG VARBINARY);", wantNear: "LONG", readable: "CREATE TABLE t (a MEDIUMBLOB);"},
		{name: "type synonym SERIAL", sql: "CREATE TABLE t (a SERIAL);", wantNear: "SERIAL", readable: "CREATE TABLE t (a BIGINT UNSIGNED NOT NULL AUTO_INCREMENT UNIQUE);"},
		{name: "attribute SERIAL DEFAULT VALUE", sql: "CREATE TABLE t (a INT SERIAL DEFAULT VALUE);", wantNear: "SERIAL", readable: "CREATE TABLE t (a INT NOT NULL AUTO_INCREMENT UNIQUE);"},
		{name: "KEY as the first entry", sql: "CREATE TABLE t (KEY (a), a INT);", wantNear: "KEY", readable: "CREATE TABLE t (a INT, KEY (a));"},
		{name: "PRIMARY KEY as the first entry", sql: "CREATE TABLE t (PRIMARY KEY (a), a INT);", wantNear: "PRIMARY", readable: "CREATE TABLE t (a INT, PRIMARY KEY (a));"},
		{name: "UNIQUE as the first entry", sql: "CREATE TABLE t (UNIQUE (a), a INT);", wantNear: "UNIQUE", readable: "CREATE TABLE t (a INT, UNIQUE (a));"},
		{name: "FOREIGN KEY as the first entry", sql: parent + "CREATE TABLE t (FOREIGN KEY (a) REFERENCES p (id), a INT);", wantNear: "FOREIGN", readable: parent + "CREATE TABLE t (a INT, FOREIGN KEY (a) REFERENCES p (id));"},
		{name: "CHECK on the first column", sql: "CREATE TABLE t (a INT CHECK (a > 0), b INT);", wantNear: "CHECK", readable: "CREATE TABLE t (b INT, a INT CHECK (a > 0));"},
		{name: "two CHECKs on one column", sql: "CREATE TABLE t (a INT, b INT CHECK (b > 0) CHECK (b < 9));", wantNear: "CHECK", readable: "CREATE TABLE t (a INT, b INT CHECK (b > 0), CHECK (b < 9));"},
		{name: "attribute after an inline CHECK", sql: "CREATE TABLE t (a INT, b INT CHECK (b > 0) NOT NULL);", wantNear: "NULL", readable: "CREATE TABLE t (a INT, b INT NOT NULL CHECK (b > 0));"},
		{name: "inline CHECK in ADD COLUMN", sql: "CREATE TABLE t (a INT);\nALTER TABLE t ADD COLUMN c INT CHECK (c > 0);", wantNear: "CHECK", readable: "CREATE TABLE t (a INT);\nALTER TABLE t ADD COLUMN c INT, ADD CHECK (c > 0);"},
		{name: "inline CHECK in MODIFY COLUMN", sql: "CREATE TABLE t (a INT, b INT);\nALTER TABLE t MODIFY COLUMN b INT CHECK (b > 0);", wantNear: "CHECK", readable: "CREATE TABLE t (a INT, b INT);\nALTER TABLE t MODIFY COLUMN b INT, ADD CHECK (b > 0);"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := newParser(t).Parse("schema.sql", []byte(tt.sql))
			if err == nil {
				t.Fatalf("Parse(%q) error = nil, want a syntax error", tt.sql)
			}
			if !strings.Contains(err.Error(), "syntax error") || !strings.Contains(err.Error(), "near '"+tt.wantNear+"'") {
				t.Errorf("Parse(%q) error = %v, want a syntax error near '%s'", tt.sql, err, tt.wantNear)
			}
			if err := newParser(t).Parse("schema.sql", []byte(tt.readable)); err != nil {
				t.Errorf("Parse(%q) error = %v, want the readable form to parse", tt.readable, err)
			}
		})
	}
}

// A table-level CHECK is the one key-like entry that may open a CREATE TABLE.
func TestTableLevelCheckFirstParses(t *testing.T) {
	p := newParser(t)
	if err := p.Parse("schema.sql", []byte("CREATE TABLE t (CHECK (a > 0), a INT, b INT);")); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	table := p.Schema().Tables[0]
	if diff := cmp.Diff([]string{"a", "b"}, mswColumnNames(table)); diff != "" {
		t.Errorf("columns mismatch (-want +got):\n%s", diff)
	}
	check := findConstraint(table.Constraints, "t_chk_1", parser.Check)
	if check == nil {
		t.Fatalf("CHECK t_chk_1 not found in %+v", table.Constraints)
	}
	if check.CheckExpression != "a > 0" {
		t.Errorf("CHECK t_chk_1 expression = %q, want %q", check.CheckExpression, "a > 0")
	}
	if diff := cmp.Diff([]string{"a"}, check.Columns); diff != "" {
		t.Errorf("CHECK t_chk_1 columns mismatch (-want +got):\n%s", diff)
	}
}

// mswColumnNames returns the names of table's columns in declared order.
func mswColumnNames(table parser.Table) []string {
	names := make([]string, 0, len(table.Columns))
	for _, c := range table.Columns {
		names = append(names, c.Name)
	}
	return names
}
