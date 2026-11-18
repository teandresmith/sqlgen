package cli

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// TestParseSchema_InputViewsFilesAreNotDDL_MySQLAndSQLite gives each of MySQL
// and SQLite a views file. The DDL parser leaves a trace of any file it reads:
// a CREATE TABLE adds a table, and the MySQL DDL parser warns that it skipped a
// CREATE VIEW. The MySQL view file parser takes a single statement, so only the
// SQLite file carries a CREATE TABLE.
func TestParseSchema_InputViewsFilesAreNotDDL_MySQLAndSQLite(t *testing.T) {
	tests := []struct {
		name      string
		dialect   config.Dialect
		ddl       string
		viewFile  string
		wantTable []string
		wantView  []string
	}{
		{
			name:      "mysql",
			dialect:   config.DialectMySQL,
			ddl:       "CREATE TABLE orders (id BIGINT PRIMARY KEY);",
			viewFile:  "-- @pk: id\nCREATE VIEW order_ids AS SELECT o.id FROM orders o;\n",
			wantTable: []string{"orders"},
			wantView:  []string{"order_ids"},
		},
		{
			name:      "sqlite",
			dialect:   config.DialectSQLite,
			ddl:       "CREATE TABLE orders (id INTEGER PRIMARY KEY);",
			viewFile:  "CREATE TABLE ghosts (id INTEGER PRIMARY KEY);\n-- @pk: id\nCREATE VIEW order_ids AS SELECT o.id FROM orders o;\n",
			wantTable: []string{"orders"},
			wantView:  []string{"order_ids"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := tdfParseFiles(t, config.InputConfig{Dialect: tt.dialect},
				map[string]string{"schema.sql": tt.ddl},
				map[string]string{"order_ids.sql": tt.viewFile},
			)
			tables := make([]string, 0, len(schema.Tables))
			for _, tb := range schema.Tables {
				tables = append(tables, tb.Name)
			}
			if diff := cmp.Diff(tt.wantTable, tables); diff != "" {
				t.Errorf("parseSchema() tables mismatch (-want +got):\n%s", diff)
			}
			views := make([]string, 0, len(schema.Views))
			for _, v := range schema.Views {
				views = append(views, v.Name)
			}
			if diff := cmp.Diff(tt.wantView, views); diff != "" {
				t.Errorf("parseSchema() views mismatch (-want +got):\n%s", diff)
			}
			var viewWarnings []string
			for _, w := range schema.Warnings {
				if strings.Contains(w, "order_ids") {
					viewWarnings = append(viewWarnings, w)
				}
			}
			if len(viewWarnings) != 0 {
				t.Errorf("parseSchema() warnings name the view file's view: %q", viewWarnings)
			}
		})
	}
}
