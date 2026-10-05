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

func TestMySQLGeneratedStorage(t *testing.T) {
	tests := []struct {
		name        string
		extra       string
		wantStorage string
		wantOK      bool
	}{
		{name: "stored", extra: "stored generated", wantStorage: "STORED", wantOK: true},
		{name: "virtual", extra: "virtual generated", wantStorage: "VIRTUAL", wantOK: true},
		{name: "auto increment", extra: "auto_increment"},
		{name: "expression default", extra: "default_generated"},
		{name: "plain column", extra: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage, ok := mysqlGeneratedStorage(tt.extra)
			if storage != tt.wantStorage || ok != tt.wantOK {
				t.Errorf("mysqlGeneratedStorage(%q) = (%q, %t), want (%q, %t)", tt.extra, storage, ok, tt.wantStorage, tt.wantOK)
			}
		})
	}
}

// The cases use GENERATION_EXPRESSION as MySQL 8.0 stores it: the text
// SHOW CREATE TABLE prints, with every quote and backslash escaped again.
func TestUnescapeMySQLCatalogText(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "no escapes", text: "(`qty` * 2)", want: "(`qty` * 2)"},
		{name: "quoted literal", text: `concat(upper(` + "`name`" + `),_latin1\'-x\')`, want: "concat(upper(`name`),_latin1'-x')"},
		{name: "escaped quote in a literal", text: `_latin1\'it\\\'s\'`, want: `_latin1'it\'s'`},
		{name: "escaped backslash in a literal", text: `_latin1\'a\\\\b\'`, want: `_latin1'a\\b'`},
		{name: "escaped newline in a literal", text: `_latin1\'c\\nd\'`, want: `_latin1'c\nd'`},
		{name: "double quote is not escaped", text: `_latin1\'e"f\'`, want: `_latin1'e"f'`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := unescapeMySQLCatalogText(tt.text); got != tt.want {
				t.Errorf("unescapeMySQLCatalogText(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}
