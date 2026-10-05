package config_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// TestValidatePostParse_CursorKeys_ReadsResolvedPK pins the PK-override rule
// that lives entirely in validation: §4.13's PK fallback must read the *resolved*
// primary key, which includes a tables.<name>.primary_key.columns override
// (PRD §8.6), not the raw schema flags.
//
// Reading the raw flags warned "no primary key is available; Connection method
// omitted" for every override table whose columns miss the inherited cursor
// key, while the generator went on to emit Connection anyway — the warning was
// simply false. Both shipped override fixtures (counters, rate_limits) tripped
// it.
func TestValidatePostParse_CursorKeys_ReadsResolvedPK(t *testing.T) {
	// `counters` carries no `id` column, so the built-in cursor_keys default
	// does not resolve against it and §4.13 falls through to the PK.
	counters := config.SchemaTable{
		Name:   "counters",
		Schema: "public",
		Columns: []config.SchemaColumn{
			{Name: "key", Type: "text"},
			{Name: "count", Type: "bigint"},
		},
		UniqueGroups: [][]string{{"key"}},
	}

	const warning = "no primary key is available; Connection method omitted"

	tests := []struct {
		name        string
		pkOverride  []string
		wantWarning bool
	}{
		{
			name:        "override supplies the PK fallback",
			pkOverride:  []string{"key"},
			wantWarning: false,
		},
		{
			name:        "no override and no schema PK still warns",
			pkOverride:  nil,
			wantWarning: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tableCfg := config.TableConfig{}
			if tt.pkOverride != nil {
				tableCfg.PrimaryKey = &config.TablePrimaryKeyConfig{Columns: tt.pkOverride}
			}
			cfg.Tables["public.counters"] = tableCfg

			warnings, err := config.ValidatePostParse(cfg, []config.SchemaTable{counters}, nil)
			if err != nil {
				t.Fatalf("ValidatePostParse() unexpected error: %v", err)
			}

			var got bool
			for _, w := range warnings {
				if strings.Contains(w.Message, warning) && strings.Contains(w.Message, "counters") {
					got = true
					break
				}
			}
			if got != tt.wantWarning {
				t.Errorf("ValidatePostParse() cursor_keys warning for counters = %t, want %t\nwarnings: %v",
					got, tt.wantWarning, warnings)
			}
		})
	}
}
