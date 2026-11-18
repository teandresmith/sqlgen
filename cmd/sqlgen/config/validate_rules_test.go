package config_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// vrCheckErr reports whether err matches want: nil when want is empty, and an
// error containing want otherwise. fn names the validator for the message.
func vrCheckErr(t *testing.T, fn string, err error, want string) {
	t.Helper()
	if want == "" {
		if err != nil {
			t.Errorf("%s() error = %v, want nil", fn, err)
		}
		return
	}
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("%s() error = %v, want it to contain %q", fn, err, want)
	}
}

// vrTenantedConfig returns a valid config with global tenancy switched on, so
// a per-entity tenancy block is judged on its own merits.
func vrTenantedConfig() *config.RootConfig {
	cfg := validConfig()
	cfg.Tenancy = &config.TenancyConfig{Enabled: true, Column: "workspace_id"}
	return cfg
}

// TestValidatePreParse_TenancyTypeImportAtEachScope pins that the TypeOverride
// import rule reaches tenancy.type on a table and a view as well as globally:
// builtins, same-package types and built-in literals need no import, a package
// sqlgen does not know does, and a package-qualified nullable variant needs one
// when its parent carries none to inherit.
func TestValidatePreParse_TenancyTypeImportAtEachScope(t *testing.T) {
	types := []struct {
		name    string
		typ     config.TypeOverride
		wantErr string // suffix after the scope's path; empty for valid
	}{
		{name: "builtin", typ: config.TypeOverride{Type: "string"}},
		{name: "same-package type", typ: config.TypeOverride{Type: "WorkspaceID"}},
		{name: "built-in literal with its own import", typ: config.TypeOverride{Type: "time.Time"}},
		{name: "sqlgen types literal", typ: config.TypeOverride{Type: "types.JSON"}},
		{name: "unknown package with an import", typ: config.TypeOverride{Type: "ksuid.KSUID", Import: "github.com/segmentio/ksuid"}},
		{
			name:    "unknown package without an import",
			typ:     config.TypeOverride{Type: "ksuid.KSUID"},
			wantErr: `.type: "ksuid.KSUID" names a package-qualified type — set `,
		},
		{
			name:    "package-qualified nullable under a builtin parent",
			typ:     config.TypeOverride{Type: "string", Nullable: config.NullableVariant{Type: "sql.NullString"}},
			wantErr: `.nullable.type: "sql.NullString" names a package-qualified type`,
		},
		{
			name: "package-qualified nullable with its own import",
			typ: config.TypeOverride{
				Type:     "string",
				Nullable: config.NullableVariant{Type: "sql.NullString", Import: "database/sql"},
			},
		},
		{
			name: "nullable from another package than the parent",
			typ: config.TypeOverride{
				Type: "uuid.UUID", Import: "github.com/google/uuid",
				Nullable: config.NullableVariant{Type: "types.JSON"},
			},
			wantErr: `.nullable.type: "types.JSON" names a package-qualified type`,
		},
		{
			name: "nullable inheriting the parent import",
			typ: config.TypeOverride{
				Type: "uuid.UUID", Import: "github.com/google/uuid",
				Nullable: config.NullableVariant{Type: "uuid.NullUUID"},
			},
		},
	}
	scopes := []struct {
		name  string
		path  string
		apply func(cfg *config.RootConfig, typ *config.TypeOverride)
	}{
		{
			name:  "global",
			path:  "tenancy.type",
			apply: func(cfg *config.RootConfig, typ *config.TypeOverride) { cfg.Tenancy.Type = typ },
		},
		{
			name: "table",
			path: "tables.products.tenancy.type",
			apply: func(cfg *config.RootConfig, typ *config.TypeOverride) {
				cfg.Tables["products"] = config.TableConfig{Tenancy: &config.TableTenancyConfig{Type: typ}}
			},
		},
		{
			name: "view",
			path: "views.project_stats.tenancy.type",
			apply: func(cfg *config.RootConfig, typ *config.TypeOverride) {
				cfg.Views["project_stats"] = config.ViewConfig{
					StructName: "ProjectStat",
					Tenancy:    &config.TableTenancyConfig{Type: typ},
				}
			},
		},
	}
	for _, scope := range scopes {
		for _, tt := range types {
			t.Run(scope.name+"/"+tt.name, func(t *testing.T) {
				cfg := vrTenantedConfig()
				typ := tt.typ
				scope.apply(cfg, &typ)
				_, err := config.ValidatePreParse(cfg)
				want := ""
				if tt.wantErr != "" {
					want = scope.path + tt.wantErr
				}
				vrCheckErr(t, "ValidatePreParse", err, want)
			})
		}
	}
}

// TestValidatePreParse_TableOverridesUsePointersRejected pins that the
// per-table overrides block takes types only: the pointer policy is
// package-wide, so a table-level use_pointers of either value is an error that
// points at the global key.
func TestValidatePreParse_TableOverridesUsePointersRejected(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(cfg *config.RootConfig)
		wantErr string
	}{
		{
			name: "use_pointers false on a table",
			mutate: func(cfg *config.RootConfig) {
				cfg.Tables["users"] = config.TableConfig{Overrides: &config.OverrideConfig{UsePointers: new(false)}}
			},
			wantErr: "tables.users.overrides.use_pointers: not allowed per table — tables.users.overrides takes types only, and a package has one pointer policy; set overrides.use_pointers globally instead",
		},
		{
			name: "use_pointers true on a table",
			mutate: func(cfg *config.RootConfig) {
				cfg.Tables["users"] = config.TableConfig{Overrides: &config.OverrideConfig{UsePointers: new(true)}}
			},
			wantErr: "tables.users.overrides.use_pointers",
		},
		{
			name: "types only on a table",
			mutate: func(cfg *config.RootConfig) {
				cfg.Tables["users"] = config.TableConfig{Overrides: &config.OverrideConfig{
					Types: map[string]config.TypeOverride{"text": {Type: "string"}},
				}}
			},
		},
		{
			name:   "use_pointers set globally",
			mutate: func(cfg *config.RootConfig) { cfg.Overrides.UsePointers = new(false) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(cfg)
			_, err := config.ValidatePreParse(cfg)
			vrCheckErr(t, "ValidatePreParse", err, tt.wantErr)
		})
	}
}

// TestValidatePreParse_TenancyOptInRequiresGlobalSwitch pins that a per-table
// or per-view tenancy.enabled: true is an error while global tenancy is off or
// absent — no tenancy code is generated then — and that the opt-out and the
// opt-in under a global on stay valid.
func TestValidatePreParse_TenancyOptInRequiresGlobalSwitch(t *testing.T) {
	globals := []struct {
		name    string
		tenancy *config.TenancyConfig
		on      bool
	}{
		{name: "no tenancy block", tenancy: nil},
		{name: "global off", tenancy: &config.TenancyConfig{Enabled: false, Column: "workspace_id"}},
		{name: "global on", tenancy: &config.TenancyConfig{Enabled: true, Column: "workspace_id"}, on: true},
	}
	entities := []struct {
		name  string
		path  string
		apply func(cfg *config.RootConfig, enabled bool)
	}{
		{
			name: "table",
			path: "tables.users.tenancy.enabled",
			apply: func(cfg *config.RootConfig, enabled bool) {
				cfg.Tables["users"] = config.TableConfig{Tenancy: &config.TableTenancyConfig{Enabled: new(enabled)}}
			},
		},
		{
			name: "view",
			path: "views.project_stats.tenancy.enabled",
			apply: func(cfg *config.RootConfig, enabled bool) {
				cfg.Views["project_stats"] = config.ViewConfig{
					StructName: "ProjectStat",
					Tenancy:    &config.TableTenancyConfig{Enabled: new(enabled)},
				}
			},
		},
	}
	for _, g := range globals {
		for _, e := range entities {
			for _, enabled := range []bool{true, false} {
				name := e.name + " opt-out under " + g.name
				if enabled {
					name = e.name + " opt-in under " + g.name
				}
				t.Run(name, func(t *testing.T) {
					cfg := validConfig()
					cfg.Tenancy = g.tenancy
					e.apply(cfg, enabled)
					_, err := config.ValidatePreParse(cfg)
					want := ""
					if enabled && !g.on {
						want = e.path + ": cannot be true when global tenancy.enabled is false — no tenancy code is generated while the global switch is off"
					}
					vrCheckErr(t, "ValidatePreParse", err, want)
				})
			}
		}
	}
}

// TestValidatePreParse_EmptyCursorKeysRejected pins that an explicit empty
// cursor_keys list is an error at each of its three scopes, while an omitted
// key (nil) and a non-empty list stay valid.
func TestValidatePreParse_EmptyCursorKeysRejected(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(cfg *config.RootConfig)
		wantErr string
	}{
		{
			name:    "empty global list",
			mutate:  func(cfg *config.RootConfig) { cfg.Generation.CursorKeys = []string{} },
			wantErr: "generation.cursor_keys: must not be empty — a cursor needs a key; omit cursor_keys for the default [id]",
		},
		{
			name: "empty table list",
			mutate: func(cfg *config.RootConfig) {
				cfg.Tables["users"] = config.TableConfig{CursorKeys: []string{}}
			},
			wantErr: "tables.users.cursor_keys: must not be empty — a cursor needs a key; omit cursor_keys to inherit generation.cursor_keys",
		},
		{
			name: "empty view list",
			mutate: func(cfg *config.RootConfig) {
				cfg.Views["project_stats"] = config.ViewConfig{StructName: "ProjectStat", CursorKeys: []string{}}
			},
			wantErr: "views.project_stats.cursor_keys: must not be empty — a cursor needs a key; omit cursor_keys to inherit generation.cursor_keys",
		},
		{name: "omitted everywhere", mutate: func(*config.RootConfig) {}},
		{
			name: "non-empty at every scope",
			mutate: func(cfg *config.RootConfig) {
				cfg.Generation.CursorKeys = []string{"id"}
				cfg.Tables["users"] = config.TableConfig{CursorKeys: []string{"created_at", "id"}}
				cfg.Views["project_stats"] = config.ViewConfig{StructName: "ProjectStat", CursorKeys: []string{"id"}}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(cfg)
			_, err := config.ValidatePreParse(cfg)
			vrCheckErr(t, "ValidatePreParse", err, tt.wantErr)
		})
	}
}

// TestLoadConfig_EmptyCursorKeysReachValidation pins the loader half of the
// empty cursor_keys rule: a YAML `cursor_keys: []` arrives at validation as an
// empty list rather than being defaulted or read as omitted, and an omitted
// global key still takes the default.
func TestLoadConfig_EmptyCursorKeysReachValidation(t *testing.T) {
	const base = "input:\n  dialect: postgres\n  paths: [\"./migrations\"]\noutput:\n  dir: ./models\n"
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{name: "global", yaml: "generation:\n  cursor_keys: []\n", wantErr: "generation.cursor_keys: must not be empty"},
		{name: "table", yaml: "tables:\n  users:\n    cursor_keys: []\n", wantErr: "tables.users.cursor_keys: must not be empty"},
		{name: "view", yaml: "views:\n  project_stats:\n    cursor_keys: []\n", wantErr: "views.project_stats.cursor_keys: must not be empty"},
		{name: "omitted", yaml: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := config.LoadConfig(writeTestConfig(t, base+tt.yaml))
			if err != nil {
				t.Fatalf("LoadConfig() error = %v, want the config to decode", err)
			}
			_, err = config.ValidatePreParse(cfg)
			vrCheckErr(t, "ValidatePreParse", err, tt.wantErr)
		})
	}
}

// TestValidatePostParse_ViewInvalidateOnExcludeTablesPatterns pins which
// exclude_tables patterns make a cached view's invalidate_on entry an error:
// a bare or schema-qualified match is, a non-matching pattern is not, and a
// view whose cache is off is not checked.
func TestValidatePostParse_ViewInvalidateOnExcludeTablesPatterns(t *testing.T) {
	tables := []config.SchemaTable{
		{Name: "products", Schema: "public", Columns: []config.SchemaColumn{{Name: "id", Type: "uuid"}}},
		{Name: "events", Schema: "audit", Columns: []config.SchemaColumn{{Name: "id", Type: "uuid"}}},
	}
	tests := []struct {
		name     string
		patterns []string
		cached   bool
		wantErr  string
	}{
		{
			name:     "bare pattern match",
			patterns: []string{"eve*"},
			cached:   true,
			wantErr:  `views.product_summary.invalidate_on: table "audit.events" is excluded from generation by exclude_tables pattern "eve*"; view cache would silently never invalidate from this source`,
		},
		{
			name:     "schema-qualified pattern match",
			patterns: []string{"audit.*"},
			cached:   true,
			wantErr:  `views.product_summary.invalidate_on: table "audit.events" is excluded from generation by exclude_tables pattern "audit.*"`,
		},
		{name: "pattern matching nothing named", patterns: []string{"logs_*"}, cached: true},
		{name: "view cache off", patterns: []string{"audit.*"}, cached: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.ExcludeTables = tt.patterns
			cfg.Views["product_summary"] = config.ViewConfig{
				StructName:   "ProductSummary",
				Cache:        &config.ViewCacheConfig{Enabled: new(tt.cached)},
				InvalidateOn: []string{"products", "audit.events"},
			}
			_, err := config.ValidatePostParse(cfg, tables, nil)
			vrCheckErr(t, "ValidatePostParse", err, tt.wantErr)
		})
	}
}

// TestValidatePreParse_EnumAndTypeOutputStayInTheModelsPackage pins that
// output.enums and output.types must land in the models package: the file,
// resolved relative to output.dir unless absolute, sits directly in
// output.dir, and a set package equals output.package. Generated models
// reference those types unqualified, so anything else does not build.
func TestValidatePreParse_EnumAndTypeOutputStayInTheModelsPackage(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	subpath := filepath.Join("sub", "custom_gen.go")
	outside := filepath.Join(root, "shared", "custom_gen.go")
	tests := []struct {
		name string
		file string
		pkg  string
		// wantErr is the expected message for output.<key>, or nil for valid.
		wantErr func(key string) string
	}{
		{name: "relative name in output.dir", file: "custom_gen.go"},
		{name: "relative name with a dot segment", file: "./custom_gen.go"},
		{name: "absolute path into output.dir", file: filepath.Join(root, "models", "custom_gen.go")},
		{
			name: "relative subpath",
			file: subpath,
			wantErr: func(key string) string {
				return fmt.Sprintf("output.%[1]s.file: %[2]q is outside output.dir \"./models\" — generated models do not import a separate %[1]s package; keep the file in output.dir", key, subpath)
			},
		},
		{
			name: "absolute path outside output.dir",
			file: outside,
			wantErr: func(key string) string {
				return fmt.Sprintf("output.%s.file: %q is outside output.dir \"./models\"", key, outside)
			},
		},
		{name: "package equal to output.package", file: "custom_gen.go", pkg: "models"},
		{
			name: "package differing from output.package",
			file: "custom_gen.go",
			pkg:  "shared",
			wantErr: func(key string) string {
				return fmt.Sprintf("output.%[1]s.package: \"shared\" differs from output.package \"models\" — generated models do not import a separate %[1]s package; omit output.%[1]s.package or set it to \"models\"", key)
			},
		},
	}
	for _, key := range []string{"enums", "types"} {
		for _, tt := range tests {
			t.Run(key+"/"+tt.name, func(t *testing.T) {
				cfg := validConfig()
				cfg.Output.Enums = &config.EnumOutputConfig{File: "enums_gen.go", Package: "models"}
				cfg.Output.Types = &config.TypeOutputConfig{File: "types_gen.go", Package: "models"}
				if key == "enums" {
					cfg.Output.Enums = &config.EnumOutputConfig{File: tt.file, Package: tt.pkg}
				} else {
					cfg.Output.Types = &config.TypeOutputConfig{File: tt.file, Package: tt.pkg}
				}
				_, err := config.ValidatePreParse(cfg)
				want := ""
				if tt.wantErr != nil {
					want = tt.wantErr(key)
				}
				vrCheckErr(t, "ValidatePreParse", err, want)
			})
		}
	}
}
