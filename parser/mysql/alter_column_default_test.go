package mysql_test

import (
	"strings"
	"testing"
)

// TestAlterColumnSetDropDefault pins that ALTER TABLE … ALTER COLUMN … SET
// DEFAULT and DROP DEFAULT change the column's default, as MySQL applies them.
func TestAlterColumnSetDropDefault(t *testing.T) {
	const base = "CREATE TABLE t (id int PRIMARY KEY, a int DEFAULT 7);\n"
	tests := []struct {
		name    string
		sql     string
		want    string
		wantErr string
	}{
		{name: "set default", sql: "ALTER TABLE t ALTER COLUMN a SET DEFAULT 5;", want: "5"},
		{name: "drop default", sql: "ALTER TABLE t ALTER COLUMN a DROP DEFAULT;", want: ""},
		{name: "set default without column keyword", sql: "ALTER TABLE t ALTER a SET DEFAULT 5;", want: "5"},
		{name: "set default null is no default", sql: "ALTER TABLE t ALTER COLUMN a SET DEFAULT NULL;", want: ""},
		{name: "column named in another case", sql: "ALTER TABLE t ALTER COLUMN A SET DEFAULT 5;", want: "5"},
		{name: "set invisible keeps the default", sql: "ALTER TABLE t ALTER COLUMN a SET INVISIBLE;", want: "7"},
		{name: "set invisible on an unknown column", sql: "ALTER TABLE t ALTER COLUMN b SET INVISIBLE;", wantErr: "alter column t.b: column not found"},
		{name: "unknown column", sql: "ALTER TABLE t ALTER COLUMN b SET DEFAULT 5;", wantErr: "alter column t.b: column not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newParser(t)
			err := p.Parse("schema.sql", []byte(base+tt.sql))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Parse error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			col := p.Schema().Tables[0].Columns[1]
			if col.Name != "a" || col.Default != tt.want {
				t.Errorf("column %s default = %q, want %q", col.Name, col.Default, tt.want)
			}
		})
	}
}
