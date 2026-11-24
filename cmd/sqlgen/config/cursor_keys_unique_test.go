package config_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// TestValidatePostParse_CursorKeys_Uniqueness pins the PRD §4.13 warning for
// resolved cursor_keys that cover no primary key or UNIQUE constraint.
// Connection seeks strictly past the last edge's key values (§14.2), so rows
// sharing a key value at a page boundary are skipped. The check reads the keys
// the generator resolves, after exclude_columns, and skips tables generation
// drops.
func TestValidatePostParse_CursorKeys_Uniqueness(t *testing.T) {
	products := config.SchemaTable{
		Name:   "products",
		Schema: "public",
		Columns: []config.SchemaColumn{
			{Name: "id", Type: "bigint", PrimaryKey: true},
			{Name: "category_id", Type: "bigint"},
			{Name: "title", Type: "text"},
			{Name: "sku", Type: "text", Unique: true},
		},
		UniqueGroups: [][]string{{"id"}, {"sku"}, {"category_id", "title"}},
		PrimaryKey:   []string{"id"},
	}
	// readings has no PK; the built-in default [id] names a plain column.
	readings := config.SchemaTable{
		Name:   "readings",
		Schema: "public",
		Columns: []config.SchemaColumn{
			{Name: "id", Type: "bigint"},
			{Name: "value", Type: "numeric"},
		},
	}
	const warning = "cover no primary key or UNIQUE constraint"

	tests := []struct {
		name          string
		table         config.SchemaTable
		globalKeys    []string
		tableCfg      config.TableConfig
		excludeTables []string
		want          string // the warning's table and keys, empty for none
	}{
		{
			name:     "explicit non-unique key warns",
			table:    products,
			tableCfg: config.TableConfig{CursorKeys: []string{"category_id"}},
			want:     "cursor_keys [category_id] on table public.products",
		},
		{
			name:       "inherited non-unique key warns",
			table:      products,
			globalKeys: []string{"category_id"},
			want:       "cursor_keys [category_id] on table public.products",
		},
		{
			name:     "keys ending in the primary key are silent",
			table:    products,
			tableCfg: config.TableConfig{CursorKeys: []string{"category_id", "id"}},
		},
		{
			name:  "built-in default on the primary key is silent",
			table: products,
		},
		{
			name:     "a UNIQUE column is silent",
			table:    products,
			tableCfg: config.TableConfig{CursorKeys: []string{"sku"}},
		},
		{
			name:     "a covered UNIQUE group is silent",
			table:    products,
			tableCfg: config.TableConfig{CursorKeys: []string{"title", "category_id"}},
		},
		{
			name:       "inherited key removed by exclude_columns falls back to the PK silently",
			table:      products,
			globalKeys: []string{"category_id"},
			tableCfg:   config.TableConfig{ExcludeColumns: []string{"category_id"}},
		},
		{
			name:     "primary_key.columns override covered by the keys is silent",
			table:    readings,
			tableCfg: config.TableConfig{PrimaryKey: &config.TablePrimaryKeyConfig{Columns: []string{"id"}}},
		},
		{
			name:  "primary_key.columns override not covered by the keys warns",
			table: readings,
			tableCfg: config.TableConfig{
				PrimaryKey: &config.TablePrimaryKeyConfig{Columns: []string{"id"}},
				CursorKeys: []string{"value"},
			},
			want: "cursor_keys [value] on table public.readings",
		},
		{
			name:  "a table skipped for having no primary key is silent",
			table: readings,
		},
		{
			name:          "a table dropped by exclude_tables is silent",
			table:         products,
			tableCfg:      config.TableConfig{CursorKeys: []string{"category_id"}},
			excludeTables: []string{"products"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Generation.CursorKeys = tt.globalKeys
			cfg.ExcludeTables = tt.excludeTables
			cfg.Tables["public."+tt.table.Name] = tt.tableCfg

			warnings, err := config.ValidatePostParse(cfg, []config.SchemaTable{tt.table}, nil)
			if err != nil {
				t.Fatalf("ValidatePostParse() unexpected error: %v", err)
			}
			var got []string
			for _, w := range warnings {
				if strings.Contains(w.Message, warning) {
					got = append(got, w.Message)
				}
			}
			switch {
			case tt.want == "" && len(got) > 0:
				t.Errorf("ValidatePostParse() uniqueness warnings = %q, want none", got)
			case tt.want != "" && (len(got) != 1 || !strings.Contains(got[0], tt.want)):
				t.Errorf("ValidatePostParse() uniqueness warnings = %q, want one containing %q", got, tt.want)
			}
		})
	}
}
