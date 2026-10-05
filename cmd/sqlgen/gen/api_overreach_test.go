package gen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// TestAPIOverReachWarnings pins the over-reach rule (PRD §4.13, §26.5.1): an
// explicit per-table `api.operations` key set true for a method the table's
// client does not generate is ignored, and warns, naming the missing schema
// fact. It is read off the table's resolved operations — exactly the
// schema-allowed set — so it runs at resolution level, under both `validate`
// (ValidateGeneration) and `generate` (GenerateInto). A global mask stays
// silent, and so does a key the schema does allow.
func TestAPIOverReachWarnings(t *testing.T) {
	const marker = "because the table's client does not generate it"
	tests := []struct {
		name       string
		generation string // extra `generation:` body
		globalMask string // `api.operations` entry; empty sets none
		tableMask  string // `tables.users.api.operations` entry; empty sets none
		softDelete bool   // users carries a deleted_at column
		appKey     bool   // users' key is app-enforced, so it has no conflict target
		want       string // the expected warning; empty for none
	}{
		{
			name:      "soft_delete without a soft-delete column",
			tableMask: "soft_delete: true",
			want:      `tables.users.api.operations.soft_delete: the API cannot expose "soft_delete" because the table's client does not generate it — the table has no soft-delete column; the key is ignored (the mask is subtractive; PRD §26.5.1)`,
		},
		{
			name:      "restore without a soft-delete column",
			tableMask: "restore: true",
			want:      `tables.users.api.operations.restore: the API cannot expose "restore" because the table's client does not generate it — the table has no soft-delete column; the key is ignored (the mask is subtractive; PRD §26.5.1)`,
		},
		{
			name:      "create_with_related with nested mutations off",
			tableMask: "create_with_related: true",
			want:      `tables.users.api.operations.create_with_related: the API cannot expose "create_with_related" because the table's client does not generate it — generation.nested_mutations does not enable the create family; the key is ignored (the mask is subtractive; PRD §26.5.1)`,
		},
		{
			name:       "update_with_related with its family not listed",
			generation: "  nested_mutations:\n    enabled: true\n    operations: [create]\n",
			tableMask:  "update_with_related: true",
			want:       `tables.users.api.operations.update_with_related: the API cannot expose "update_with_related" because the table's client does not generate it — generation.nested_mutations does not enable the update family; the key is ignored (the mask is subtractive; PRD §26.5.1)`,
		},
		{
			name:       "create_with_related on a table with no eligible edge",
			generation: "  nested_mutations:\n    enabled: true\n",
			tableMask:  "create_with_related: true",
			want:       `tables.users.api.operations.create_with_related: the API cannot expose "create_with_related" because the table's client does not generate it — the table has no relationship eligible for a nested mutation (PRD §9.9.4); the key is ignored (the mask is subtractive; PRD §26.5.1)`,
		},
		{
			name:       "upsert_with_related on a table with no conflict target",
			generation: "  nested_mutations:\n    enabled: true\n",
			tableMask:  "upsert_with_related: true",
			appKey:     true,
			want:       `tables.users.api.operations.upsert_with_related: the API cannot expose "upsert_with_related" because the table's client does not generate it — the table has no conflict target; the key is ignored (the mask is subtractive; PRD §26.5.1)`,
		},
		{
			name:       "a global mask over-reaching stays silent",
			globalMask: "soft_delete: true",
		},
		{
			name:       "a key the schema allows stays silent",
			tableMask:  "soft_delete: true",
			softDelete: true,
		},
		{
			name:      "a key set false stays silent",
			tableMask: "soft_delete: false",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			build := func() (*parser.Schema, *config.RootConfig) {
				body := "version: v1\ninput:\n  dialect: postgres\n  paths:\n    - ./schema.sql\n" +
					"output:\n  driver: pgx\n  dir: ./models\n  package: models\n"
				if tt.generation != "" {
					body += "generation:\n" + tt.generation
				}
				body += "api:\n  enabled: true\n  graphql:\n    enabled: true\n    schema_dir: ./models/graph\n" +
					"    resolver_dir: ./models/graph\n    package: graph\n    field_casing: camel_case\n"
				if tt.globalMask != "" {
					body += "  operations:\n    " + tt.globalMask + "\n"
				}
				if tt.tableMask != "" || tt.appKey {
					body += "tables:\n  users:\n"
				}
				if tt.appKey {
					body += "    primary_key:\n      columns: [id]\n"
				}
				if tt.tableMask != "" {
					body += "    api:\n      operations:\n        " + tt.tableMask + "\n"
				}
				cfgPath := filepath.Join(t.TempDir(), "sqlgen.yml")
				if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
					t.Fatalf("writing config: %v", err)
				}
				cfg, err := config.LoadConfig(cfgPath)
				if err != nil {
					t.Fatalf("LoadConfig: %v", err)
				}
				columns := []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "email", Type: "text"},
				}
				if tt.softDelete {
					columns = append(columns, parser.Column{Name: "deleted_at", Type: "timestamptz", Nullable: true})
				}
				return &parser.Schema{Tables: []parser.Table{{Name: "users", Schema: "public", Columns: columns}}}, cfg
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
					if strings.Contains(w, marker) {
						hits = append(hits, w)
					}
				}
				switch {
				case tt.want == "" && len(hits) != 0:
					t.Errorf("%s: over-reach warnings = %q, want none", got.cmd, hits)
				case tt.want != "" && (len(hits) != 1 || hits[0] != tt.want):
					t.Errorf("%s: over-reach warnings = %q, want exactly [%q]", got.cmd, hits, tt.want)
				}
			}
		})
	}
}
