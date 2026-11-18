package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// tftLoad writes body under a minimal postgres input/output header and loads
// it, failing the test on a load error.
func tftLoad(t *testing.T, body string) *config.RootConfig {
	t.Helper()
	cfg, err := config.LoadConfig(writeTestConfig(t, tftHeader+body))
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}
	return cfg
}

// tftLoadConnection loads a postgres config whose input.connection block is
// conn, indented under `connection:`.
func tftLoadConnection(t *testing.T, conn string) *config.RootConfig {
	t.Helper()
	body := "\ninput:\n  dialect: postgres\n  paths: [\"./migrations\"]\n"
	if conn != "" {
		body += "  connection:\n" + conn
	}
	body += "output:\n  dir: ./models\n"
	cfg, err := config.LoadConfig(writeTestConfig(t, body))
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}
	return cfg
}

const tftHeader = `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models
`

// TestLoadConfig_EntityCacheRejectsSerializer pins that the per-table (and,
// with the same fields, per-view) cache override carries only `enabled` and
// `ttl`: a `serializer` key there is an unknown key, not an override.
func TestLoadConfig_EntityCacheRejectsSerializer(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "per-table serializer is rejected",
			yaml: `
cache:
  enabled: true
tables:
  products:
    cache:
      ttl: "30m"
      serializer: msgpack
`,
			wantErr: "field serializer not found in type config.TableCacheConfig",
		},
		{
			name: "per-view serializer is rejected",
			yaml: `
cache:
  enabled: true
views:
  product_summary:
    invalidate_on: [products]
    cache:
      enabled: true
      serializer: msgpack
`,
			wantErr: "field serializer not found in type config.ViewCacheConfig",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.LoadConfig(writeTestConfig(t, tftHeader+tt.yaml))
			if err == nil {
				t.Fatalf("LoadConfig() error = nil, want %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("LoadConfig() error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// TestViewCacheOptIn pins that a view is cached only when it sets
// `cache.enabled: true` and lists a non-empty `invalidate_on`: an enabled view
// with no sources is a validation error, and a view with sources but no
// opt-in is not cached even under a global `cache.enabled: true`.
func TestViewCacheOptIn(t *testing.T) {
	const wantErr = "views.product_summary.cache.enabled is true but invalidate_on is empty"
	tests := []struct {
		name       string
		yaml       string
		wantErr    bool
		wantCached bool
	}{
		{
			name: "enabled with empty invalidate_on is a validation error",
			yaml: `
views:
  product_summary:
    cache:
      enabled: true
`,
			wantErr: true,
		},
		{
			name: "invalidate_on without the view opt-in is not cached",
			yaml: `
views:
  product_summary:
    invalidate_on: [products]
`,
			wantCached: false,
		},
		{
			name: "enabled with a non-empty invalidate_on is cached",
			yaml: `
views:
  product_summary:
    invalidate_on: [products]
    cache:
      enabled: true
`,
			wantCached: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tftLoad(t, "cache:\n  enabled: true\n  key_prefix: app\n"+tt.yaml)
			_, err := config.ValidatePreParse(cfg)
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), wantErr) {
					t.Errorf("ValidatePreParse() error = %v, want it to contain %q", err, wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidatePreParse() unexpected error: %v", err)
			}
			if got := config.ResolveViewCacheEnabled(cfg.Views["product_summary"], cfg.Cache); got != tt.wantCached {
				t.Errorf("ResolveViewCacheEnabled() = %v, want %v", got, tt.wantCached)
			}
		})
	}
}

// TestResolveViewCacheTTL_InheritsGlobal pins that a cached view with no
// `ttl` of its own takes the global `cache.ttl`, set here to a value other
// than the 1h default.
func TestResolveViewCacheTTL_InheritsGlobal(t *testing.T) {
	cfg := tftLoad(t, `
cache:
  enabled: true
  key_prefix: app
  ttl: "2h"
views:
  product_summary:
    invalidate_on: [products]
    cache:
      enabled: true
`)
	if got := config.ResolveViewCacheTTL(cfg.Views["product_summary"], cfg.Cache); got != 2*time.Hour {
		t.Errorf("ResolveViewCacheTTL() = %s, want 2h (the global ttl)", got)
	}
}

// TestLoadConfig_EnvPasswordIgnoredByURL pins that SQLGEN_DB_PASSWORD has no
// effect on a connection that comes from a `url`: the url is kept verbatim.
func TestLoadConfig_EnvPasswordIgnoredByURL(t *testing.T) {
	const fileURL = "postgres://app@db.internal:5432/appdb?sslmode=disable"
	t.Setenv("SQLGEN_DB_URL", "")
	t.Setenv("SQLGEN_DB_PASSWORD", "env-secret")
	cfg := tftLoadConnection(t, `    url: `+fileURL+`
`)
	if got := cfg.Input.Connection.URL; got != fileURL {
		t.Errorf("input.connection.url = %q, want %q unchanged by SQLGEN_DB_PASSWORD", got, fileURL)
	}
}

// TestLoadConfig_EnvOverridesAfterExpansion pins that both overrides are
// applied after ${VAR} expansion: the override replaces the value the file's
// reference expanded to.
func TestLoadConfig_EnvOverridesAfterExpansion(t *testing.T) {
	t.Run("SQLGEN_DB_PASSWORD replaces an expanded password", func(t *testing.T) {
		t.Setenv("SQLGEN_DB_URL", "")
		t.Setenv("TFT_FILE_PASSWORD", "file-secret")
		t.Setenv("SQLGEN_DB_PASSWORD", "env-secret")
		cfg := tftLoadConnection(t, `    host: localhost
    user: app
    database: appdb
    password: ${TFT_FILE_PASSWORD}
`)
		if got := cfg.Input.Connection.Password; got != "env-secret" {
			t.Errorf("input.connection.password = %q, want %q", got, "env-secret")
		}
	})
	t.Run("SQLGEN_DB_URL replaces an expanded url", func(t *testing.T) {
		t.Setenv("SQLGEN_DB_PASSWORD", "")
		t.Setenv("TFT_FILE_URL", "postgres://file@db.internal/filedb")
		t.Setenv("SQLGEN_DB_URL", "postgres://env@db.internal/envdb")
		cfg := tftLoadConnection(t, `    url: ${TFT_FILE_URL}
`)
		if got := cfg.Input.Connection.URL; got != "postgres://env@db.internal/envdb" {
			t.Errorf("input.connection.url = %q, want %q", got, "postgres://env@db.internal/envdb")
		}
	})
}

// TestLoadConfig_EnvOverrideValuesNotExpanded pins that neither override's
// own value is expanded: a ${VAR} inside it is kept literally.
func TestLoadConfig_EnvOverrideValuesNotExpanded(t *testing.T) {
	t.Run("SQLGEN_DB_PASSWORD value is literal", func(t *testing.T) {
		t.Setenv("SQLGEN_DB_URL", "")
		t.Setenv("TFT_INNER", "expanded")
		t.Setenv("SQLGEN_DB_PASSWORD", "${TFT_INNER}")
		cfg := tftLoadConnection(t, `    host: localhost
    user: app
    database: appdb
`)
		if got := cfg.Input.Connection.Password; got != "${TFT_INNER}" {
			t.Errorf("input.connection.password = %q, want the literal %q", got, "${TFT_INNER}")
		}
	})
	t.Run("SQLGEN_DB_URL value is literal", func(t *testing.T) {
		t.Setenv("SQLGEN_DB_PASSWORD", "")
		t.Setenv("TFT_INNER", "expanded")
		t.Setenv("SQLGEN_DB_URL", "postgres://app:${TFT_INNER}@db.internal/appdb")
		cfg := tftLoadConnection(t, "")
		if got := cfg.Input.Connection.URL; got != "postgres://app:${TFT_INNER}@db.internal/appdb" {
			t.Errorf("input.connection.url = %q, want the literal %q", got, "postgres://app:${TFT_INNER}@db.internal/appdb")
		}
	})
}

// TestValidatePostParse_BareKeyNeverAmbiguousWithoutSchemas pins that on
// MySQL and SQLite a bare `tables:` key never raises the ambiguity error. The
// fixture holds the same table name twice under two schemas — the exact shape
// that is ambiguous on PostgreSQL, which the control case confirms.
func TestValidatePostParse_BareKeyNeverAmbiguousWithoutSchemas(t *testing.T) {
	const ambiguous = `ambiguous table reference "products"`
	tables := []config.SchemaTable{
		{Name: "products", Schema: "app", Columns: []config.SchemaColumn{{Name: "id", Type: "bigint", PrimaryKey: true}}},
		{Name: "products", Schema: "billing", Columns: []config.SchemaColumn{{Name: "id", Type: "bigint", PrimaryKey: true}}},
	}
	tests := []struct {
		name          string
		dialect       config.Dialect
		driver        config.Driver
		wantAmbiguous bool
	}{
		{name: "mysql bare key over a repeated table name", dialect: config.DialectMySQL, driver: config.DriverStdlib},
		{name: "sqlite bare key over a repeated table name", dialect: config.DialectSQLite, driver: config.DriverStdlib},
		{name: "postgres bare key over a repeated table name is ambiguous", dialect: config.DialectPostgres, driver: config.DriverPgx, wantAmbiguous: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Input.Dialect = tt.dialect
			cfg.Output.Driver = tt.driver
			cfg.Tables["products"] = config.TableConfig{}

			_, err := config.ValidatePostParse(cfg, tables, nil)
			got := err != nil && strings.Contains(err.Error(), ambiguous)
			if got != tt.wantAmbiguous {
				t.Errorf("ValidatePostParse() error = %v, want ambiguity error %v", err, tt.wantAmbiguous)
			}
		})
	}
}
