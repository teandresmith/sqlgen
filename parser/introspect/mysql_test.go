package introspect

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"
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
		{name: "display width before 8.0.19", columnType: "bigint(20)", want: "bigint"},
		{name: "unsigned display width before 8.0.19", columnType: "int(10) unsigned", want: "int unsigned"},
		{name: "zerofill keeps its width", columnType: "int(5) unsigned zerofill", want: "int(5) unsigned zerofill"},
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

// The cases use COLUMN_DEFAULT and EXTRA as MySQL 8.0 stores them: a literal
// default unquoted, an expression default escaped like GENERATION_EXPRESSION
// and marked DEFAULT_GENERATED.
func TestMySQLDefault(t *testing.T) {
	tests := []struct {
		name       string
		dflt       sql.NullString
		extra      string
		columnType string
		want       string
	}{
		{name: "no default", columnType: "int", want: ""},
		{name: "empty string", dflt: sql.NullString{Valid: true}, columnType: "varchar(50)", want: "''"},
		{name: "string literal", dflt: sql.NullString{String: "active", Valid: true}, columnType: "varchar(50)", want: "'active'"},
		{name: "quote in a literal", dflt: sql.NullString{String: "it's", Valid: true}, columnType: "char(4)", want: `'it\'s'`},
		{name: "backslash in a literal", dflt: sql.NullString{String: `a\b`, Valid: true}, columnType: "text", want: `'a\\b'`},
		{name: "keyword text in a string column", dflt: sql.NullString{String: "CURRENT_TIMESTAMP", Valid: true}, columnType: "varchar(20)", want: "'CURRENT_TIMESTAMP'"},
		{name: "enum literal", dflt: sql.NullString{String: "in active", Valid: true}, columnType: "enum('active','in active')", want: "'in active'"},
		{name: "set literal", dflt: sql.NullString{String: "r,w", Valid: true}, columnType: "set('r','w')", want: "'r,w'"},
		{name: "datetime literal", dflt: sql.NullString{String: "2020-01-01 00:00:00", Valid: true}, columnType: "datetime", want: "'2020-01-01 00:00:00'"},
		{name: "date literal", dflt: sql.NullString{String: "2020-01-01", Valid: true}, columnType: "date", want: "'2020-01-01'"},
		{name: "time literal", dflt: sql.NullString{String: "12:00:00", Valid: true}, columnType: "time(2)", want: "'12:00:00'"},
		{name: "integer", dflt: sql.NullString{String: "0", Valid: true}, columnType: "int", want: "0"},
		{name: "negative integer", dflt: sql.NullString{String: "-1", Valid: true}, columnType: "int", want: "-1"},
		{name: "decimal", dflt: sql.NullString{String: "1.50", Valid: true}, columnType: "decimal(10,2)", want: "1.50"},
		{name: "bit literal", dflt: sql.NullString{String: "b'101'", Valid: true}, columnType: "bit(8)", want: "0b101"},
		{name: "year", dflt: sql.NullString{String: "2020", Valid: true}, columnType: "year", want: "2020"},
		{name: "binary literal reads as hex", dflt: sql.NullString{String: "0x6162", Valid: true}, columnType: "varbinary(4)", want: "0x6162"},
		{name: "current timestamp", dflt: sql.NullString{String: "CURRENT_TIMESTAMP", Valid: true}, extra: "DEFAULT_GENERATED on update CURRENT_TIMESTAMP", columnType: "timestamp", want: "current_timestamp()"},
		{name: "current timestamp with precision", dflt: sql.NullString{String: "CURRENT_TIMESTAMP(3)", Valid: true}, extra: "DEFAULT_GENERATED", columnType: "datetime(3)", want: "current_timestamp(3)"},
		{name: "function", dflt: sql.NullString{String: "uuid()", Valid: true}, extra: "DEFAULT_GENERATED", columnType: "varchar(36)", want: "uuid()"},
		{name: "parenthesized expression", dflt: sql.NullString{String: "(1 + 2)", Valid: true}, extra: "DEFAULT_GENERATED", columnType: "int", want: "1 + 2"},
		{name: "expression with a literal", dflt: sql.NullString{String: `_utf8mb4\'{}\'`, Valid: true}, extra: "DEFAULT_GENERATED", columnType: "json", want: "'{}'"},
		{name: "function with literals", dflt: sql.NullString{String: `concat(_utf8mb4\'a\',_utf8mb4\'b\')`, Valid: true}, extra: "DEFAULT_GENERATED", columnType: "varchar(20)", want: "concat('a', 'b')"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mysqlDefault(tt.dflt, tt.extra, tt.columnType)
			if err != nil {
				t.Fatalf("mysqlDefault(%+v, %q, %q) error: %v", tt.dflt, tt.extra, tt.columnType, err)
			}
			if got != tt.want {
				t.Errorf("mysqlDefault(%+v, %q, %q) = %q, want %q", tt.dflt, tt.extra, tt.columnType, got, tt.want)
			}
		})
	}
}

func TestMySQLDefault_UnparsableExpression(t *testing.T) {
	dflt := sql.NullString{String: "uuid(", Valid: true}
	if _, err := mysqlDefault(dflt, "DEFAULT_GENERATED", "varchar(36)"); err == nil {
		t.Errorf("mysqlDefault(%+v) error = nil, want a parse error", dflt)
	}
}

// Only error 1109 (unknown table: no CHECK catalog before MySQL 8.0.16)
// means the server has no CHECK constraints to read.
func TestCheckQueryError(t *testing.T) {
	unknownTable := &mysqldriver.MySQLError{Number: 1109, Message: "Unknown table 'CHECK_CONSTRAINTS' in information_schema"}
	tests := []struct {
		name    string
		err     error
		wantNil bool
	}{
		{name: "unknown table", err: unknownTable, wantNil: true},
		{name: "wrapped unknown table", err: fmt.Errorf("query: %w", unknownTable), wantNil: true},
		{name: "access denied", err: &mysqldriver.MySQLError{Number: 1142, Message: "SELECT command denied"}},
		{name: "cancelled context", err: context.Canceled},
		{name: "bad connection", err: mysqldriver.ErrInvalidConn},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkQueryError("users", tt.err)
			if tt.wantNil {
				if got != nil {
					t.Errorf("checkQueryError(%v) = %v, want nil", tt.err, got)
				}
				return
			}
			if !errors.Is(got, tt.err) {
				t.Errorf("checkQueryError(%v) = %v, want an error wrapping it", tt.err, got)
			}
		})
	}
}
