package introspect

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestNormalizeMySQLType(t *testing.T) {
	tests := []struct {
		name       string
		columnType string
		want       string
	}{
		{name: "int", columnType: "int", want: "int"},
		{name: "int unsigned", columnType: "int unsigned", want: "int unsigned"},
		{name: "tinyint(1)", columnType: "tinyint(1)", want: "tinyint(1)"},
		{name: "display width before 8.0.19", columnType: "bigint(20)", want: "bigint(20)"},
		{name: "decimal", columnType: "decimal(10,2)", want: "decimal(10,2)"},
		{name: "enum is bare", columnType: "enum('a','b')", want: "enum"},
		{name: "set is bare", columnType: "set('r','w')", want: "set"},
		{name: "mixed case", columnType: "VarChar(255)", want: "varchar(255)"},
		{name: "mixed-case enum", columnType: "ENUM('A','B')", want: "enum"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeMySQLType(tt.columnType); got != tt.want {
				t.Errorf("normalizeMySQLType(%q) = %q, want %q", tt.columnType, got, tt.want)
			}
		})
	}
}

// The cases use COLUMN_TYPE as MySQL 8.0 renders it: a quote is doubled and
// a backslash or newline is backslash-escaped.
func TestExtractMySQLEnumValues(t *testing.T) {
	tests := []struct {
		name       string
		columnType string
		want       []string
	}{
		{name: "plain list", columnType: "enum('active','inactive','pending')", want: []string{"active", "inactive", "pending"}},
		{name: "value with a comma", columnType: "enum('active','in,active')", want: []string{"active", "in,active"}},
		{name: "doubled quote", columnType: "enum('it''s')", want: []string{"it's"}},
		{name: "empty value", columnType: "enum('a','')", want: []string{"a", ""}},
		{name: "values with spaces", columnType: "enum('on hold','in review')", want: []string{"on hold", "in review"}},
		{name: "escaped backslash", columnType: `enum('b\\c')`, want: []string{`b\c`}},
		{name: "escaped newline", columnType: `enum('n\nl')`, want: []string{"n\nl"}},
		{name: "double quote", columnType: `enum('q"r')`, want: []string{`q"r`}},
		{name: "set", columnType: "set('r','w')", want: []string{"r", "w"}},
		{name: "not an enum", columnType: "int", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractMySQLEnumValues(tt.columnType)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("extractMySQLEnumValues(%q) mismatch (-want +got):\n%s", tt.columnType, diff)
			}
		})
	}
}
