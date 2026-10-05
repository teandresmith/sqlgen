package config_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// withColumnName returns validConfig() carrying a single
// `column_map.<col>.name` override on the users table.
func withColumnName(col, name string) *config.RootConfig {
	cfg := validConfig()
	cfg.Tables["users"] = config.TableConfig{
		ColumnMap: map[string]config.ColumnOverride{
			col: {Name: name},
		},
	}
	return cfg
}

// TestValidatePreParse_ColumnName_Identifier pins the §8.5 rule that the
// override must be a valid *exported* Go identifier. The PascalCase form of a
// Go keyword is deliberately accepted: the reserved-word escape applies only
// to generated locals, never to exposed struct fields.
func TestValidatePreParse_ColumnName_Identifier(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		wantErr string
	}{
		{name: "acronym fix accepted", field: "APIKey"},
		{name: "digit-leading rename accepted", field: "FiscalYear2010Revenue"},
		{name: "capitalized keyword accepted", field: "Type"},
		{name: "underscore accepted", field: "Client_IP"},
		{name: "empty is unset", field: ""},
		{
			name:    "space rejected",
			field:   "Api Key",
			wantErr: `tables.users.column_map.api_key.name: "Api Key" is not a valid Go identifier`,
		},
		{
			name:    "dotted path rejected",
			field:   "models.APIKey",
			wantErr: `tables.users.column_map.api_key.name: "models.APIKey" is not a valid Go identifier`,
		},
		{
			name:    "digit-leading rejected",
			field:   "2010Revenue",
			wantErr: `tables.users.column_map.api_key.name: "2010Revenue" is not a valid Go identifier`,
		},
		{
			name:    "bare keyword rejected",
			field:   "type",
			wantErr: `tables.users.column_map.api_key.name: "type" is not a valid Go identifier`,
		},
		{
			name:    "unexported rejected",
			field:   "apiKey",
			wantErr: `tables.users.column_map.api_key.name: "apiKey" must be exported`,
		},
		{
			name:    "underscore-leading rejected as unexported",
			field:   "_APIKey",
			wantErr: `tables.users.column_map.api_key.name: "_APIKey" must be exported`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.ValidatePreParse(withColumnName("api_key", tt.field))
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("ValidatePreParse() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidatePreParse() error = nil, want %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ValidatePreParse() error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// TestValidatePreParse_ColumnName_ExcludedColumn verifies that renaming a
// column exclude_columns removes is an error — there is no field to rename.
// Both list levels are covered because they merge as a union (PRD §4.8).
func TestValidatePreParse_ColumnName_ExcludedColumn(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(cfg *config.RootConfig)
	}{
		{
			name: "table-level exclude_columns",
			mutate: func(cfg *config.RootConfig) {
				tc := cfg.Tables["users"]
				tc.ExcludeColumns = []string{"api_key"}
				cfg.Tables["users"] = tc
			},
		},
		{
			name: "global exclude_columns",
			mutate: func(cfg *config.RootConfig) {
				cfg.Generation.ExcludeColumns = []string{"api_key"}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := withColumnName("api_key", "APIKey")
			tt.mutate(cfg)
			_, err := config.ValidatePreParse(cfg)
			want := `tables.users.column_map.api_key.name: column "api_key" is removed by exclude_columns`
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("ValidatePreParse() error = %v, want it to contain %q", err, want)
			}
		})
	}
}

// TestValidatePostParse_ColumnName_ColumnExists verifies the schema-phase
// rule: an override naming a column that is not on the table is an error
// rather than a silent no-op.
func TestValidatePostParse_ColumnName_ColumnExists(t *testing.T) {
	tables := []config.SchemaTable{
		{
			Name:   "users",
			Schema: "public",
			Columns: []config.SchemaColumn{
				{Name: "id", Type: "bigint", PrimaryKey: true, AutoIncrement: true},
				{Name: "api_key", Type: "text"},
			},
		},
	}

	tests := []struct {
		name    string
		column  string
		wantErr string
	}{
		{name: "existing column accepted", column: "api_key"},
		{
			name:    "unknown column rejected",
			column:  "apikey",
			wantErr: `tables.public.users.column_map.apikey.name: column "apikey" does not exist on the table`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := withColumnName(tt.column, "APIKey")
			_, err := config.ValidatePostParse(cfg, tables, nil)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("ValidatePostParse() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidatePostParse() error = nil, want %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ValidatePostParse() error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// TestValidatePreParse_ColumnName_BatchReporting verifies that every bad
// override is reported in one pass rather than one per run.
func TestValidatePreParse_ColumnName_BatchReporting(t *testing.T) {
	cfg := validConfig()
	cfg.Tables["users"] = config.TableConfig{
		ColumnMap: map[string]config.ColumnOverride{
			"api_key":  {Name: "api key"},
			"ip_addr":  {Name: "clientIP"},
			"osi_code": {Name: "OSICode"},
		},
	}
	_, err := config.ValidatePreParse(cfg)
	if err == nil {
		t.Fatal("ValidatePreParse() error = nil, want two errors")
	}
	for _, want := range []string{"api_key.name", "ip_addr.name"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ValidatePreParse() error = %q, want it to contain %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "osi_code") {
		t.Errorf("ValidatePreParse() error = %q, want no complaint about the valid override", err)
	}
}
