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

// tftGenConfig loads a postgres config whose output goes to outDir, with
// extra appended at the root.
func tftGenConfig(t *testing.T, outDir, extra string) *config.RootConfig {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "sqlgen.yml")
	body := "version: v1\ninput:\n  dialect: postgres\n  paths:\n    - ./schema.sql\n" +
		"output:\n  driver: pgx\n  dir: " + outDir + "\n  package: db\n" + extra
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	return cfg
}

// TestBuildEntityContexts_BareKeyMatchesSingleSchemaTable pins that a bare
// `tables:` key applies to a table in a non-default schema when no other
// schema has a table of that name.
func TestBuildEntityContexts_BareKeyMatchesSingleSchemaTable(t *testing.T) {
	cfg := tenancyTestConfig()
	cfg.Tenancy = nil
	cfg.Tables["trail"] = config.TableConfig{StructName: "AuditEntry"}
	schema := &parser.Schema{
		Tables: []parser.Table{
			{Name: "trail", Schema: "audit", Columns: []parser.Column{tableCol("id", "bigint", false, true)}},
			{Name: "users", Schema: "public", Columns: []parser.Column{tableCol("id", "bigint", false, true)}},
		},
	}

	tables, _, err := gen.BuildEntityContextsFromSchema(schema, cfg)
	if err != nil {
		t.Fatalf("BuildEntityContextsFromSchema() error = %v, want nil", err)
	}
	got := make(map[string]string, len(tables))
	for _, tc := range tables {
		got[tc.Schema+"."+tc.TableName] = tc.StructName
	}
	if got["audit.trail"] != "AuditEntry" {
		t.Errorf("audit.trail StructName = %q, want %q from the bare `trail` key (all: %v)", got["audit.trail"], "AuditEntry", got)
	}
}

// TestBuildEntityContexts_TableTenantColumnIgnoresNullableVariant pins that a
// `tenancy.type` nullable variant is never consulted for a table's tenant
// column: the NOT NULL column takes the plain type, and a nullable one is
// still the table-tenant-column error rather than taking the variant.
func TestBuildEntityContexts_TableTenantColumnIgnoresNullableVariant(t *testing.T) {
	withVariant := func() *config.RootConfig {
		cfg := tenancyTestConfig()
		cfg.Overrides.Types["uuid"] = uuidGoogle
		cfg.Tenancy.Type = &config.TypeOverride{
			Type:     "uuid.UUID",
			Import:   "github.com/google/uuid",
			Nullable: config.NullableVariant{Type: "uuid.NullUUID", UnderlyingField: "UUID"},
		}
		return cfg
	}

	t.Run("not null table tenant column takes the plain type", func(t *testing.T) {
		cfg := withVariant()
		tables, _, err := gen.BuildEntityContextsFromSchema(tenantTypeSchema(), cfg)
		if err != nil {
			t.Fatalf("BuildEntityContextsFromSchema() error = %v, want nil", err)
		}
		tenant := tenantColumnOf(t, tables[0].Columns, "workspace_id")
		if tenant.GoType != "uuid.UUID" {
			t.Errorf("table column workspace_id GoType = %q, want %q (the nullable variant must not apply to a table)", tenant.GoType, "uuid.UUID")
		}
	})

	t.Run("nullable table tenant column is still rejected", func(t *testing.T) {
		cfg := withVariant()
		schema := &parser.Schema{Tables: []parser.Table{{
			Name:   "projects",
			Schema: "public",
			Columns: []parser.Column{
				tableCol("id", "uuid", false, true),
				tableCol("workspace_id", "uuid", true, false),
			},
		}}}
		_, _, err := gen.BuildTenancyContext(cfg, schema, tenancyTestResolver(cfg))
		if err == nil || !strings.Contains(err.Error(), "workspace_id") || !strings.Contains(err.Error(), "nullable") {
			t.Errorf("BuildTenancyContext() error = %v, want the nullable table tenant column error naming workspace_id", err)
		}
	})
}

// tftDivergentSchema is one uuid-tenanted table beside a table and a view
// whose tenant column is bigint, so the uniform-type check fails unless the
// bigint entities are opted out.
func tftDivergentSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{Name: "users", Schema: "public", Columns: []parser.Column{
				tableCol("id", "uuid", false, true),
				tableCol("workspace_id", "uuid", false, false),
			}},
			{Name: "legacy_widgets", Schema: "public", Columns: []parser.Column{
				tableCol("id", "bigint", false, true),
				tableCol("workspace_id", "bigint", false, false),
			}},
		},
		Views: []parser.View{{Name: "legacy_rollup", Schema: "public", Columns: []parser.Column{
			viewCol("id", "bigint", false),
			viewCol("workspace_id", "bigint", false),
		}}},
	}
}

// TestGenerate_TenancyOptOutLeavesUniformTypeCheck pins that opting an entity
// out of tenancy removes it from the uniform-type check, so a schema whose
// tenant column types diverge then generates.
func TestGenerate_TenancyOptOutLeavesUniformTypeCheck(t *testing.T) {
	const tenancy = "tenancy:\n  enabled: true\n  column: workspace_id\n"
	tests := []struct {
		name    string
		extra   string
		wantErr bool
	}{
		{
			name:    "divergent table and view without an opt-out are rejected",
			extra:   tenancy,
			wantErr: true,
		},
		{
			name:    "opting the divergent table and view out generates",
			extra:   tenancy + "tables:\n  legacy_widgets:\n    tenancy:\n      enabled: false\nviews:\n  legacy_rollup:\n    tenancy:\n      enabled: false\n",
			wantErr: false,
		},
		{
			name:    "opting out only the table still rejects the divergent view",
			extra:   tenancy + "tables:\n  legacy_widgets:\n    tenancy:\n      enabled: false\n",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outDir := t.TempDir()
			cfg := tftGenConfig(t, outDir, tt.extra)
			_, err := gen.Generate(tftDivergentSchema(), cfg, "test")
			const uniform = "tenanted entities must resolve to a uniform tenant Go type"
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), uniform) {
					t.Errorf("Generate() error = %v, want the uniform-type error", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Generate() error = %v, want nil", err)
			}
			body := readGenerated(t, filepath.Join(outDir, "models_gen.go"))
			if !strings.Contains(body, "tenancy.TenantResolver[uuid.UUID]") {
				t.Errorf("models_gen.go missing tenancy.TenantResolver[uuid.UUID]; the remaining uuid-tenanted table must still be scoped")
			}
		})
	}
}

// TestGenerate_TenantedViewLeavesCacheOutputUnchanged pins that making a
// cached view tenanted changes nothing in the cache layer: cache_gen.go is
// byte-identical whether the view is tenanted by detection or opted out, so
// the key grammar, the affected-tenant carriers and the view fingerprint are
// all unchanged.
func TestGenerate_TenantedViewLeavesCacheOutputUnchanged(t *testing.T) {
	schema := func() *parser.Schema {
		return &parser.Schema{
			Tables: []parser.Table{{Name: "products", Schema: "public", Columns: []parser.Column{
				tableCol("id", "uuid", false, true),
				tableCol("workspace_id", "uuid", false, false),
				tableCol("name", "text", false, false),
			}}},
			Views: []parser.View{{Name: "product_summary", Schema: "public", Columns: []parser.Column{
				viewCol("id", "uuid", false),
				viewCol("workspace_id", "uuid", false),
				viewCol("name", "text", false),
			}}},
		}
	}
	const base = "cache:\n  enabled: true\n  key_prefix: app\ntenancy:\n  enabled: true\n  column: workspace_id\n"
	const viewCache = "    invalidate_on: [products]\n    cache:\n      enabled: true\n"

	generate := func(t *testing.T, viewBlock string) (cacheGen, viewsGen string) {
		t.Helper()
		outDir := t.TempDir()
		cfg := tftGenConfig(t, outDir, base+"views:\n  product_summary:\n"+viewBlock+viewCache)
		if _, err := gen.Generate(schema(), cfg, "test"); err != nil {
			t.Fatalf("Generate() error = %v", err)
		}
		return readGenerated(t, filepath.Join(outDir, "cache_gen.go")), readGenerated(t, filepath.Join(outDir, "views_gen.go"))
	}

	tenantedCache, tenantedViews := generate(t, "")
	sharedCache, sharedViews := generate(t, "    tenancy:\n      enabled: false\n")

	// The fixture only means something if the view really is scoped in one
	// run and not in the other.
	if tenantedViews == sharedViews {
		t.Fatal("views_gen.go is identical with and without view tenancy; the fixture does not make the view tenanted")
	}
	if tenantedCache != sharedCache {
		t.Errorf("cache_gen.go differs when the view is tenanted; a tenanted view must not change the cache layer\n--- tenanted ---\n%s\n--- opted out ---\n%s", tenantedCache, sharedCache)
	}
}
