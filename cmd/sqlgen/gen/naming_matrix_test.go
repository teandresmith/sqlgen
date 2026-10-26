package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// nmTableContext builds the context of one table named sqlName, carrying an
// extra column also named sqlName, so one build answers both the struct and
// the field question for the same SQL spelling.
func nmTableContext(t *testing.T, sqlName string) gen.TableContext {
	t.Helper()
	table := namedTable(sqlName, "")
	table.Columns = append(table.Columns, parser.Column{Name: sqlName, Type: "text"})
	contexts, err := gen.BuildTableContextsFromSchema(&parser.Schema{Tables: []parser.Table{table}}, nameRuleConfig())
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema(%q) error = %v", sqlName, err)
	}
	if len(contexts) != 1 {
		t.Fatalf("BuildTableContextsFromSchema(%q) built %d contexts, want 1", sqlName, len(contexts))
	}
	return contexts[0]
}

// nmFieldName returns the Go field name the context gave the column sqlName.
func nmFieldName(t *testing.T, tc gen.TableContext, sqlName string) string {
	t.Helper()
	for _, c := range tc.Columns {
		if c.Name == sqlName {
			return c.FieldName
		}
	}
	t.Fatalf("table %s has no column %q", tc.TableName, sqlName)
	return ""
}

// TestNamingAcronymExamples resolves each SQL name of the acronym examples as
// a table, giving the singular struct and file stem, and as a column, giving
// the field spelled in the examples table.
func TestNamingAcronymExamples(t *testing.T) {
	tests := []struct {
		name       string
		structName string
		field      string
		stem       string
	}{
		{name: "user_id", structName: "UserID", field: "UserID", stem: "user_id"},
		{name: "ip_address", structName: "IPAddress", field: "IPAddress", stem: "ip_address"},
		{name: "api_url", structName: "APIURL", field: "APIURL", stem: "api_url"},
		{name: "http_status", structName: "HTTPStatus", field: "HTTPStatus", stem: "http_status"},
		{name: "json_data", structName: "JSONDatum", field: "JSONData", stem: "json_datum"},
		{name: "ssl_enabled", structName: "SSLEnabled", field: "SSLEnabled", stem: "ssl_enabled"},
		{name: "cpu_usage", structName: "CPUUsage", field: "CPUUsage", stem: "cpu_usage"},
		{name: "ttl_seconds", structName: "TTLSecond", field: "TTLSeconds", stem: "ttl_second"},
		{name: "mac_addr", structName: "MACAddr", field: "MACAddr", stem: "mac_addr"},
		{name: "csv_data", structName: "CSVDatum", field: "CSVData", stem: "csv_datum"},
		{name: "ascii_art", structName: "ASCIIArt", field: "ASCIIArt", stem: "ascii_art"},
		{name: "sku_code", structName: "SKUCode", field: "SKUCode", stem: "sku_code"},
		{name: "utf8_body", structName: "Utf8Body", field: "Utf8Body", stem: "utf8_body"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := nmTableContext(t, tt.name)
			t.Run("struct", func(t *testing.T) {
				if tc.StructName != tt.structName {
					t.Errorf("table %q StructName = %q, want %q", tt.name, tc.StructName, tt.structName)
				}
			})
			t.Run("field", func(t *testing.T) {
				if got := nmFieldName(t, tc, tt.name); got != tt.field {
					t.Errorf("column %q FieldName = %q, want %q", tt.name, got, tt.field)
				}
			})
			t.Run("file stem", func(t *testing.T) {
				if tc.SnakeName != tt.stem {
					t.Errorf("table %q SnakeName = %q, want %q", tt.name, tc.SnakeName, tt.stem)
				}
			})
		})
	}
}

// TestNamingDigitLeadingTable resolves a table whose name begins with a digit:
// its struct, its table-name constant and its file stem all take the guard.
// The stem is checked for the guard only; where its underscores fall is not
// part of the contract.
func TestNamingDigitLeadingTable(t *testing.T) {
	tests := []struct {
		name       string
		structName string
		constant   string
		stemPrefix string
	}{
		{name: "2024_partitions", structName: "Col2024Partition", constant: "TableCol2024Partitions", stemPrefix: "col2024"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := nmTableContext(t, tt.name)
			t.Run("struct", func(t *testing.T) {
				if tc.StructName != tt.structName {
					t.Errorf("table %q StructName = %q, want %q", tt.name, tc.StructName, tt.structName)
				}
			})
			t.Run("table name constant", func(t *testing.T) {
				if tc.TableNameConstant != tt.constant {
					t.Errorf("table %q TableNameConstant = %q, want %q", tt.name, tc.TableNameConstant, tt.constant)
				}
			})
			t.Run("file stem", func(t *testing.T) {
				if !strings.HasPrefix(tc.SnakeName, tt.stemPrefix) {
					t.Errorf("table %q SnakeName = %q, want the %q guard", tt.name, tc.SnakeName, tt.stemPrefix)
				}
			})
		})
	}
}
