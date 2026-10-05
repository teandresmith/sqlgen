package config_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// withColumnType returns validConfig() carrying a single
// `column_map.<col>` type override on the users table.
func withColumnType(col, goType, imp string) *config.RootConfig {
	cfg := validConfig()
	cfg.Tables["users"] = config.TableConfig{
		ColumnMap: map[string]config.ColumnOverride{
			col: {Type: goType, Import: imp},
		},
	}
	return cfg
}

// TestValidatePreParse_ColumnType_Expression pins the §8.5 rule that a
// `column_map.<col>.type` override must parse as a Go type expression. This
// is a syntax check only — whether the named type exists is the compiler's
// verdict on the generated file, since sqlgen never loads consumer packages.
func TestValidatePreParse_ColumnType_Expression(t *testing.T) {
	tests := []struct {
		name    string
		goType  string
		wantErr string
	}{
		{name: "builtin accepted", goType: "int32"},
		{name: "qualified identifier accepted", goType: "decimal.Decimal"},
		{name: "same-package type accepted", goType: "Address"},
		{name: "pointer accepted", goType: "*Address"},
		{name: "slice accepted", goType: "[]byte"},
		{name: "slice of qualified type accepted", goType: "[]decimal.Decimal"},
		{name: "map accepted", goType: "map[string]any"},
		{name: "generic instantiation accepted", goType: "omittable.Value[string]"},
		{name: "qualified generic argument accepted", goType: "null.Value[uuid.UUID]"},
		{name: "multi-argument generic accepted", goType: "pkg.Pair[string, int]"},
		{name: "sized array accepted", goType: "[16]byte"},
		{name: "empty is unset", goType: ""},
		{
			name:    "numeric literal rejected",
			goType:  "12",
			wantErr: `tables.users.column_map.balance.type: "12" is not a Go type expression`,
		},
		{
			name:    "prose rejected",
			goType:  "not a type",
			wantErr: `tables.users.column_map.balance.type: "not a type" is not a Go type expression`,
		},
		{
			name:    "call expression rejected",
			goType:  "decimal.New()",
			wantErr: `tables.users.column_map.balance.type: "decimal.New()" is not a Go type expression`,
		},
		{
			name:    "over-qualified path rejected",
			goType:  "a.b.C",
			wantErr: `tables.users.column_map.balance.type: "a.b.C" is not a Go type expression`,
		},
		{
			name:    "non-type map value rejected",
			goType:  "map[string]12",
			wantErr: `tables.users.column_map.balance.type: "map[string]12" is not a Go type expression`,
		},
		{
			name:    "non-type slice element rejected",
			goType:  "[]4",
			wantErr: `tables.users.column_map.balance.type: "[]4" is not a Go type expression`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// No import is supplied: the missing-import guardrail needs
			// gotype's registry and runs in the gen package, so the
			// qualified cases here exercise the expression rule alone.
			_, err := config.ValidatePreParse(withColumnType("balance", tt.goType, ""))
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

// TestValidatePreParse_ColumnType_ImportNeedsType verifies that `.import`
// without `.type` is an error: an import path with nothing to import for is a
// typo, and silently ignoring it would hide the typo.
func TestValidatePreParse_ColumnType_ImportNeedsType(t *testing.T) {
	_, err := config.ValidatePreParse(withColumnType("balance", "", "github.com/shopspring/decimal"))
	want := `tables.users.column_map.balance.import: "github.com/shopspring/decimal" is set without a type`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("ValidatePreParse() error = %v, want it to contain %q", err, want)
	}
}

// TestValidatePreParse_ColumnType_ExcludedColumn verifies that retyping a
// column exclude_columns removes is an error — there is no field to retype.
// Mirrors the `.name` and `.access` rules; both list levels merge as a union
// (PRD §4.8).
func TestValidatePreParse_ColumnType_ExcludedColumn(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(cfg *config.RootConfig)
	}{
		{
			name: "table-level exclude_columns",
			mutate: func(cfg *config.RootConfig) {
				tc := cfg.Tables["users"]
				tc.ExcludeColumns = []string{"balance"}
				cfg.Tables["users"] = tc
			},
		},
		{
			name: "global exclude_columns",
			mutate: func(cfg *config.RootConfig) {
				cfg.Generation.ExcludeColumns = []string{"balance"}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := withColumnType("balance", "decimal.Decimal", "github.com/shopspring/decimal")
			tt.mutate(cfg)
			_, err := config.ValidatePreParse(cfg)
			want := `tables.users.column_map.balance.type: column "balance" is removed by exclude_columns`
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("ValidatePreParse() error = %v, want it to contain %q", err, want)
			}
		})
	}
}

// TestValidatePostParse_ColumnType_ColumnExists verifies the schema-phase
// rule: a `.type` override naming a column that is not on the table is an
// error rather than a silent no-op, matching the `.name` rule.
func TestValidatePostParse_ColumnType_ColumnExists(t *testing.T) {
	tables := []config.SchemaTable{
		{
			Name:   "users",
			Schema: "public",
			Columns: []config.SchemaColumn{
				{Name: "id", Type: "bigint", PrimaryKey: true, AutoIncrement: true},
				{Name: "balance", Type: "numeric"},
			},
		},
	}

	tests := []struct {
		name    string
		column  string
		wantErr string
	}{
		{name: "existing column accepted", column: "balance"},
		{
			name:    "unknown column rejected",
			column:  "ballance",
			wantErr: `tables.public.users.column_map.ballance.type: column "ballance" does not exist on the table`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := withColumnType(tt.column, "decimal.Decimal", "github.com/shopspring/decimal")
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

// TestValidatePreParse_TypeMap_Expression pins that `type_map` values get the
// same syntax rule `column_map.<col>.type` gets — both are written verbatim
// into the generated struct. The import rule the two forms differ on is
// enforced in the gen package, which owns the type registry.
func TestValidatePreParse_TypeMap_Expression(t *testing.T) {
	tests := []struct {
		name    string
		literal string
		wantErr string
	}{
		{name: "builtin accepted", literal: "int32"},
		{name: "same-package type accepted", literal: "Address"},
		{name: "known qualified type accepted", literal: "json.RawMessage"},
		{
			name:    "numeric literal rejected",
			literal: "12",
			wantErr: `tables.users.type_map.balance: "12" is not a Go type expression`,
		},
		{
			name:    "empty rejected",
			literal: "",
			wantErr: `tables.users.type_map.balance: is empty`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Tables["users"] = config.TableConfig{
				TypeMap: map[string]string{"balance": tt.literal},
			}
			_, err := config.ValidatePreParse(cfg)
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
