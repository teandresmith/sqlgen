package introspect_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/mysql"
)

// mswDescribeColumns describes each column of table as
// "name type [NOT NULL] [AUTO_INCREMENT] [UNIQUE]".
func mswDescribeColumns(table *parser.Table) []string {
	out := make([]string, 0, len(table.Columns))
	for _, c := range table.Columns {
		d := c.Name + " " + c.Type
		if !c.Nullable {
			d += " NOT NULL"
		}
		if c.AutoIncrement {
			d += " AUTO_INCREMENT"
		}
		if c.Unique {
			d += " UNIQUE"
		}
		out = append(out, d)
	}
	return out
}

// mswDescribeConstraints describes each constraint of table as
// "TYPE name [columns]", with a CHECK's expression after it.
func mswDescribeConstraints(table *parser.Table) []string {
	out := make([]string, 0, len(table.Constraints))
	for _, c := range table.Constraints {
		d := c.Type.String() + " " + c.Name + " [" + strings.Join(c.Columns, ",") + "]"
		if c.Type == parser.Check {
			d += " " + c.CheckExpression
		}
		out = append(out, d)
	}
	return out
}

// MySQL introspection reads information_schema rather than re-parsing table
// DDL, so it reads every table whose DDL the file parser rejects.
func TestMySQLIntrospect_ReadsDDLTheFileParserRejects(t *testing.T) {
	requireDatabases(t)
	tests := []struct {
		name            string
		table           string
		ddl             string
		wantColumns     []string
		wantConstraints []string
	}{
		{
			name:            "type synonyms",
			table:           "msw_synonyms",
			wantColumns:     []string{"id int NOT NULL", "dc decimal(10,2)", "fx decimal(6,1)", "dp double", "nc char(3)", "ntc char(4)", "ntv varchar(5)", "nv varchar(6)", "ch char(2)", "cv varchar(7)", "i1 tinyint", "i2 smallint", "i3 mediumint", "i4 int", "i8 bigint", "mi mediumint", "lg mediumtext", "lv mediumtext", "lb mediumblob"},
			wantConstraints: []string{"PRIMARY KEY PRIMARY [id]"},
			ddl: `CREATE TABLE msw_synonyms (
				id INT PRIMARY KEY,
				dc DEC(10,2), fx FIXED(6,1), dp DOUBLE PRECISION,
				nc NCHAR(3), ntc NATIONAL CHAR(4), ntv NATIONAL VARCHAR(5), nv NVARCHAR(6),
				ch CHARACTER(2), cv CHARACTER VARYING(7),
				i1 INT1, i2 INT2, i3 INT3, i4 INT4, i8 INT8, mi MIDDLEINT,
				lg LONG, lv LONG VARCHAR, lb LONG VARBINARY
			)`,
		},
		{
			name:            "SERIAL type",
			table:           "msw_serial",
			wantColumns:     []string{"id bigint unsigned NOT NULL AUTO_INCREMENT UNIQUE", "name varchar(20)"},
			wantConstraints: []string{"UNIQUE id [id]"},
			ddl:             "CREATE TABLE msw_serial (id SERIAL, name VARCHAR(20))",
		},
		{
			name:            "SERIAL DEFAULT VALUE attribute",
			table:           "msw_serial_default",
			wantColumns:     []string{"id int NOT NULL AUTO_INCREMENT UNIQUE", "name varchar(20)"},
			wantConstraints: []string{"UNIQUE id [id]"},
			ddl:             "CREATE TABLE msw_serial_default (id INT SERIAL DEFAULT VALUE, name VARCHAR(20))",
		},
		{
			name:            "PRIMARY KEY as the first entry",
			table:           "msw_pk_first",
			wantColumns:     []string{"id int NOT NULL", "a int"},
			wantConstraints: []string{"PRIMARY KEY PRIMARY [id]"},
			ddl:             "CREATE TABLE msw_pk_first (PRIMARY KEY (id), id INT, a INT)",
		},
		{
			name:            "KEY and UNIQUE as the first entries",
			table:           "msw_key_first",
			wantColumns:     []string{"id int NOT NULL", "a int", "b int UNIQUE"},
			wantConstraints: []string{"PRIMARY KEY PRIMARY [id]", "UNIQUE ub [b]", "INDEX ka [a]"},
			ddl:             "CREATE TABLE msw_key_first (KEY ka (a), UNIQUE KEY ub (b), id INT PRIMARY KEY, a INT, b INT)",
		},
		{
			name:            "FOREIGN KEY as the first entry",
			table:           "msw_fk_first",
			wantColumns:     []string{"id int NOT NULL", "pid int"},
			wantConstraints: []string{"FOREIGN KEY msw_fk_first_p [pid]", "PRIMARY KEY PRIMARY [id]", "INDEX msw_fk_first_p [pid]"},
			ddl: `CREATE TABLE msw_fk_parent (id INT PRIMARY KEY);
				CREATE TABLE msw_fk_first (CONSTRAINT msw_fk_first_p FOREIGN KEY (pid) REFERENCES msw_fk_parent (id), id INT PRIMARY KEY, pid INT)`,
		},
		{
			name:            "CHECK on the first column",
			table:           "msw_check_first_column",
			wantColumns:     []string{"a int", "b int"},
			wantConstraints: []string{"CHECK msw_check_first_column_chk_1 [a] a > 0"},
			ddl:             "CREATE TABLE msw_check_first_column (a INT CHECK (a > 0), b INT)",
		},
		{
			name:            "two CHECKs on one column",
			table:           "msw_two_checks",
			wantColumns:     []string{"a int", "b int"},
			wantConstraints: []string{"CHECK msw_two_checks_chk_1 [b] b > 0", "CHECK msw_two_checks_chk_2 [b] b < 9"},
			ddl:             "CREATE TABLE msw_two_checks (a INT, b INT CHECK (b > 0) CHECK (b < 9))",
		},
		{
			name:            "attribute after an inline CHECK",
			table:           "msw_attr_after_check",
			wantColumns:     []string{"a int", "b int NOT NULL"},
			wantConstraints: []string{"CHECK msw_attr_after_check_chk_1 [b] b > 0"},
			ddl:             "CREATE TABLE msw_attr_after_check (a INT, b INT CHECK (b > 0) NOT NULL)",
		},
		{
			name:            "inline CHECK in ADD COLUMN",
			table:           "msw_check_add_column",
			wantColumns:     []string{"a int", "c int"},
			wantConstraints: []string{"CHECK msw_check_add_column_chk_1 [c] c > 0"},
			ddl: `CREATE TABLE msw_check_add_column (a INT);
				ALTER TABLE msw_check_add_column ADD COLUMN c INT CHECK (c > 0)`,
		},
		{
			name:            "inline CHECK in MODIFY COLUMN",
			table:           "msw_check_modify_column",
			wantColumns:     []string{"a int", "b int"},
			wantConstraints: []string{"CHECK msw_check_modify_column_chk_1 [b] b > 0"},
			ddl: `CREATE TABLE msw_check_modify_column (a INT, b INT);
				ALTER TABLE msw_check_modify_column MODIFY COLUMN b INT CHECK (b > 0)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := mysql.New()
			if err != nil {
				t.Fatalf("mysql.New() error: %v", err)
			}
			if err := p.Parse("schema.sql", []byte(tt.ddl)); err == nil || !strings.Contains(err.Error(), "syntax error") {
				t.Fatalf("file parser error = %v, want a syntax error", err)
			}

			applyParityFixture(t, "mysql", mysqlConnStr,
				"DROP TABLE IF EXISTS "+tt.table+"; DROP TABLE IF EXISTS msw_fk_parent", tt.ddl)
			table := findTable(t, introspectMySQL(t, tt.table), tt.table)
			if diff := cmp.Diff(tt.wantColumns, mswDescribeColumns(table)); diff != "" {
				t.Errorf("introspected columns mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantConstraints, mswDescribeConstraints(table)); diff != "" {
				t.Errorf("introspected constraints mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
