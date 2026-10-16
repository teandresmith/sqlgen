package gen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// TestUnmatchedEnumKeyWarnings pins the `enums:` key warnings that
// ValidateGeneration and GenerateInto both raise (PRD §4.11, §4.13).
func TestUnmatchedEnumKeyWarnings(t *testing.T) {
	tests := []struct {
		name    string
		dialect config.Dialect
		enums   string // the `enums:` body
		want    []string
	}{
		{
			name:    "a key matching no enum",
			dialect: config.DialectPostgres,
			enums:   "  nosuch_enum:\n    struct_name: Bogus\n",
			want:    []string{"enums.nosuch_enum: matches no enum in the schema, so the entry has no effect — check the spelling"},
		},
		{
			name:    "a bare key that matches",
			dialect: config.DialectPostgres,
			enums:   "  user_role:\n    struct_name: Role\n",
		},
		{
			name:    "a schema-qualified key that matches",
			dialect: config.DialectPostgres,
			enums:   "  public.user_role:\n    struct_name: Role\n",
		},
		{
			name:    "a key qualified with the wrong schema",
			dialect: config.DialectPostgres,
			enums:   "  audit.user_role:\n    struct_name: Role\n",
			want:    []string{"enums.audit.user_role: matches no enum in the schema, so the entry has no effect — check the spelling"},
		},
		{
			name:    "a MySQL key in the single-underscore form",
			dialect: config.DialectMySQL,
			enums:   "  users_role_enum:\n    struct_name: Role\n",
			want:    []string{"enums.users_role_enum: matches no enum in the schema, so the entry has no effect — check the spelling (a MySQL inline ENUM is named <table>__<column>_enum)"},
		},
		{
			name:    "a MySQL key naming a SET",
			dialect: config.DialectMySQL,
			enums:   "  users__perms_set:\n    struct_name: Perms\n",
			want:    []string{"enums.users__perms_set: users__perms_set is a SET type, which takes no enums: override, so the entry has no effect — rename the table or column to change its Go type name"},
		},
		{
			name:    "a MySQL key that matches",
			dialect: config.DialectMySQL,
			enums:   "  users__role_enum:\n    struct_name: Role\n",
		},
		{
			name:    "every unmatched key, in key order",
			dialect: config.DialectPostgres,
			enums:   "  zz_enum:\n    struct_name: Z\n  user_role:\n    struct_name: Role\n  aa_enum:\n    struct_name: A\n",
			want: []string{
				"enums.aa_enum: matches no enum in the schema, so the entry has no effect — check the spelling",
				"enums.zz_enum: matches no enum in the schema, so the entry has no effect — check the spelling",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			build := func() (*parser.Schema, *config.RootConfig) {
				driver := config.DriverPgx
				if tt.dialect != config.DialectPostgres {
					driver = config.DriverStdlib
				}
				body := "version: v1\ninput:\n  dialect: " + string(tt.dialect) + "\n  paths:\n    - ./schema.sql\n" +
					"output:\n  driver: " + string(driver) + "\n  dir: ./models\n  package: models\n" +
					"enums:\n" + tt.enums
				cfgPath := filepath.Join(t.TempDir(), "sqlgen.yml")
				if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
					t.Fatalf("writing config: %v", err)
				}
				cfg, err := config.LoadConfig(cfgPath)
				if err != nil {
					t.Fatalf("LoadConfig: %v", err)
				}
				if tt.dialect == config.DialectPostgres {
					return &parser.Schema{
						Tables: []parser.Table{{Name: "users", Schema: "public", Columns: []parser.Column{
							{Name: "id", Type: "uuid", PrimaryKey: true},
						}}},
						Enums: []parser.Enum{{Name: "user_role", Schema: "public", Values: []string{"admin", "viewer"}}},
					}, cfg
				}
				return &parser.Schema{
					Tables: []parser.Table{{Name: "users", Columns: []parser.Column{
						{Name: "id", Type: "int", PrimaryKey: true},
					}}},
					Enums: []parser.Enum{{Name: "users__role_enum", Values: []string{"admin", "viewer"}}},
					Sets:  []parser.Set{{Name: "users__perms_set", Values: []string{"read", "write"}}},
				}, cfg
			}

			schema, cfg := build()
			validateWarnings, err := gen.ValidateGeneration(schema, cfg)
			if err != nil {
				t.Fatalf("ValidateGeneration() error: %v", err)
			}
			schema, cfg = build()
			result, err := gen.GenerateInto(schema, cfg, "test", t.TempDir())
			if err != nil {
				t.Fatalf("GenerateInto() error: %v", err)
			}
			for _, got := range []struct {
				cmd      string
				warnings []string
			}{{"validate", validateWarnings}, {"generate", result.Warnings}} {
				var hits []string
				for _, w := range got.warnings {
					if strings.HasPrefix(w, "enums.") {
						hits = append(hits, w)
					}
				}
				if diff := cmp.Diff(tt.want, hits); diff != "" {
					t.Errorf("%s: enums: key warnings mismatch (-want +got):\n%s", got.cmd, diff)
				}
			}
		})
	}
}
