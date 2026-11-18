package gen_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/parser/mysql"
	"github.com/teandresmith/sqlgen/parser/postgres"
	"github.com/teandresmith/sqlgen/parser/sqlite"
)

// rdwLoadDefaultConfig loads a config that leaves generation.soft_delete_columns
// unset, so the built-in default list applies.
func rdwLoadDefaultConfig(t *testing.T, dialect, driver string) *config.RootConfig {
	t.Helper()
	body := fmt.Sprintf(`version: v1
input:
  dialect: %s
  paths: [./schema.sql]
output:
  driver: %s
  dir: ./models
  package: models
`, dialect, driver)
	path := filepath.Join(t.TempDir(), "sqlgen.yml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}
	return cfg
}

// rdwParse parses ddl with the dialect's file parser.
func rdwParse(t *testing.T, dialect, ddl string) *parser.Schema {
	t.Helper()
	var p parser.Parser
	switch dialect {
	case "postgres":
		p = postgres.New("public")
	case "mysql":
		var err error
		if p, err = mysql.New(); err != nil {
			t.Fatalf("mysql.New() error: %v", err)
		}
	case "sqlite":
		p = sqlite.New()
	}
	if err := p.Parse("schema.sql", []byte(ddl)); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	schema := p.Schema()
	schema.Sort()
	return schema
}

// With soft_delete_columns left at its default, each listed name is detected
// with its listed strategy, and of several listed names on one table the
// higher-priority one wins. The columns are declared lowest priority first,
// so declaration order cannot be what picks the winner.
func TestSoftDelete_DefaultList(t *testing.T) {
	dialects := []struct {
		name            string
		dialect, driver string
		timestamp, bool string // column types
	}{
		{"postgres", "postgres", "pgx", "timestamptz", "boolean"},
		{"mysql", "mysql", "stdlib", "datetime", "boolean"},
		{"sqlite", "sqlite", "stdlib", "DATETIME", "BOOLEAN"},
	}
	tests := []struct {
		name         string
		columns      []string // soft delete candidates, in declared order
		wantColumn   string
		wantStrategy config.SoftDeleteType
	}{
		{name: "deleted_at alone", columns: []string{"deleted_at"}, wantColumn: "deleted_at", wantStrategy: "timestamp"},
		{name: "deleted_datetime alone", columns: []string{"deleted_datetime"}, wantColumn: "deleted_datetime", wantStrategy: "timestamp"},
		{name: "is_deleted alone", columns: []string{"is_deleted"}, wantColumn: "is_deleted", wantStrategy: "bool"},
		{name: "deleted_flag alone", columns: []string{"deleted_flag"}, wantColumn: "deleted_flag", wantStrategy: "bool"},
		{name: "deleted alone", columns: []string{"deleted"}, wantColumn: "deleted", wantStrategy: "bool"},
		{
			name:       "deleted_at outranks every lower name",
			columns:    []string{"deleted", "deleted_flag", "is_deleted", "deleted_datetime", "deleted_at"},
			wantColumn: "deleted_at", wantStrategy: "timestamp",
		},
		{
			name:       "deleted_datetime outranks every lower name",
			columns:    []string{"deleted", "deleted_flag", "is_deleted", "deleted_datetime"},
			wantColumn: "deleted_datetime", wantStrategy: "timestamp",
		},
		{
			name:       "is_deleted outranks every lower name",
			columns:    []string{"deleted", "deleted_flag", "is_deleted"},
			wantColumn: "is_deleted", wantStrategy: "bool",
		},
		{
			name:       "deleted_flag outranks every lower name",
			columns:    []string{"deleted", "deleted_flag"},
			wantColumn: "deleted_flag", wantStrategy: "bool",
		},
	}
	for _, d := range dialects {
		t.Run(d.name, func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					ddl := "CREATE TABLE notes (id bigint PRIMARY KEY"
					if d.dialect == "sqlite" {
						ddl = "CREATE TABLE notes (id INTEGER PRIMARY KEY"
					}
					for _, col := range tt.columns {
						typ := d.bool
						if col == "deleted_at" || col == "deleted_datetime" {
							typ = d.timestamp
						}
						ddl += ", " + col + " " + typ
					}
					ddl += ");"
					schema := rdwParse(t, d.dialect, ddl)
					cfg := rdwLoadDefaultConfig(t, d.dialect, d.driver)
					tables, err := gen.BuildTableContextsFromSchema(schema, cfg)
					if err != nil {
						t.Fatalf("BuildTableContextsFromSchema() error: %v", err)
					}
					if len(tables) != 1 {
						t.Fatalf("BuildTableContextsFromSchema() gave %d tables, want 1", len(tables))
					}
					sd := tables[0].SoftDelete
					if sd == nil {
						t.Fatalf("%s: SoftDelete = nil, want column %q", ddl, tt.wantColumn)
					}
					if sd.Column != tt.wantColumn || sd.Strategy != tt.wantStrategy {
						t.Errorf("%s: SoftDelete = {%q, %q}, want {%q, %q}", ddl, sd.Column, sd.Strategy, tt.wantColumn, tt.wantStrategy)
					}
				})
			}
		})
	}
}
