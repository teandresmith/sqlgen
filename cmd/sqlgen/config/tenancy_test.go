package config_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

func TestResolveTableTenancyEnabled(t *testing.T) {
	tests := []struct {
		name   string
		table  config.TableConfig
		global *config.TenancyConfig
		want   bool
	}{
		{
			name:   "no global no table defaults to false",
			table:  config.TableConfig{},
			global: nil,
			want:   false,
		},
		{
			name:   "global enabled no table override",
			table:  config.TableConfig{},
			global: &config.TenancyConfig{Enabled: true, Column: "workspace_id"},
			want:   true,
		},
		{
			name:   "global disabled no table override",
			table:  config.TableConfig{},
			global: &config.TenancyConfig{Enabled: false},
			want:   false,
		},
		{
			name: "table override false beats global true",
			table: config.TableConfig{
				Tenancy: &config.TableTenancyConfig{Enabled: new(false)},
			},
			global: &config.TenancyConfig{Enabled: true, Column: "workspace_id"},
			want:   false,
		},
		{
			name: "table override true opts in when global disabled",
			table: config.TableConfig{
				Tenancy: &config.TableTenancyConfig{Enabled: new(true)},
			},
			global: &config.TenancyConfig{Enabled: false},
			want:   true,
		},
		{
			name: "table tenancy struct with nil enabled inherits global true",
			table: config.TableConfig{
				Tenancy: &config.TableTenancyConfig{Enabled: nil},
			},
			global: &config.TenancyConfig{Enabled: true, Column: "workspace_id"},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := config.ResolveTableTenancyEnabled(tt.table, tt.global)
			if got != tt.want {
				t.Errorf("ResolveTableTenancyEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveTableTenancyColumn(t *testing.T) {
	orgID := "org_id"

	tests := []struct {
		name   string
		table  config.TableConfig
		global *config.TenancyConfig
		want   string
	}{
		{
			name:   "no global no table returns empty",
			table:  config.TableConfig{},
			global: nil,
			want:   "",
		},
		{
			name:   "global column inherited",
			table:  config.TableConfig{},
			global: &config.TenancyConfig{Enabled: true, Column: "workspace_id"},
			want:   "workspace_id",
		},
		{
			name: "table override wins over global",
			table: config.TableConfig{
				Tenancy: &config.TableTenancyConfig{Column: &orgID},
			},
			global: &config.TenancyConfig{Enabled: true, Column: "workspace_id"},
			want:   "org_id",
		},
		{
			name: "nil per-table column inherits global",
			table: config.TableConfig{
				Tenancy: &config.TableTenancyConfig{Column: nil},
			},
			global: &config.TenancyConfig{Enabled: true, Column: "workspace_id"},
			want:   "workspace_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := config.ResolveTableTenancyColumn(tt.table, tt.global)
			if got != tt.want {
				t.Errorf("ResolveTableTenancyColumn() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveTableTenancyRequired(t *testing.T) {
	tests := []struct {
		name   string
		table  config.TableConfig
		global *config.TenancyConfig
		want   bool
	}{
		{
			name:   "no global no table defaults to true (safe default)",
			table:  config.TableConfig{},
			global: nil,
			want:   true,
		},
		{
			name:   "global required=true no override",
			table:  config.TableConfig{},
			global: &config.TenancyConfig{Enabled: true, Column: "workspace_id", Required: new(true)},
			want:   true,
		},
		{
			name:   "global required=false no override",
			table:  config.TableConfig{},
			global: &config.TenancyConfig{Enabled: true, Column: "workspace_id", Required: new(false)},
			want:   false,
		},
		{
			name: "table required=false beats global required=true",
			table: config.TableConfig{
				Tenancy: &config.TableTenancyConfig{Required: new(false)},
			},
			global: &config.TenancyConfig{Enabled: true, Column: "workspace_id", Required: new(true)},
			want:   false,
		},
		{
			name: "table required=true beats global required=false",
			table: config.TableConfig{
				Tenancy: &config.TableTenancyConfig{Required: new(true)},
			},
			global: &config.TenancyConfig{Enabled: true, Column: "workspace_id", Required: new(false)},
			want:   true,
		},
		{
			name: "nil per-table required inherits global",
			table: config.TableConfig{
				Tenancy: &config.TableTenancyConfig{Required: nil},
			},
			global: &config.TenancyConfig{Enabled: true, Column: "workspace_id", Required: new(true)},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := config.ResolveTableTenancyRequired(tt.table, tt.global)
			if got != tt.want {
				t.Errorf("ResolveTableTenancyRequired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveTableTenancyType(t *testing.T) {
	globalType := &config.TypeOverride{Type: "uuid.UUID", Import: "github.com/google/uuid"}
	perTableType := &config.TypeOverride{Type: "ids.WorkspaceID", Import: "myapp/pkg/ids"}

	tests := []struct {
		name   string
		table  config.TableConfig
		global *config.TenancyConfig
		want   *config.TypeOverride
	}{
		{
			name:   "no override returns nil",
			table:  config.TableConfig{},
			global: &config.TenancyConfig{Enabled: true, Column: "workspace_id"},
			want:   nil,
		},
		{
			name:   "global type inherited",
			table:  config.TableConfig{},
			global: &config.TenancyConfig{Enabled: true, Column: "workspace_id", Type: globalType},
			want:   globalType,
		},
		{
			name: "per-table type overrides global",
			table: config.TableConfig{
				Tenancy: &config.TableTenancyConfig{Type: perTableType},
			},
			global: &config.TenancyConfig{Enabled: true, Column: "workspace_id", Type: globalType},
			want:   perTableType,
		},
		{
			name: "nil per-table type inherits global",
			table: config.TableConfig{
				Tenancy: &config.TableTenancyConfig{Type: nil},
			},
			global: &config.TenancyConfig{Enabled: true, Column: "workspace_id", Type: globalType},
			want:   globalType,
		},
		{
			name:   "nil global returns nil",
			table:  config.TableConfig{},
			global: nil,
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := config.ResolveTableTenancyType(tt.table, tt.global)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ResolveTableTenancyType() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestValidatePreParse_TenancyRequiresColumn(t *testing.T) {
	tests := []struct {
		name      string
		tenancy   *config.TenancyConfig
		wantErr   bool
		wantInErr string
	}{
		{
			name:    "no tenancy block is valid",
			tenancy: nil,
			wantErr: false,
		},
		{
			name:    "enabled=false without column is valid",
			tenancy: &config.TenancyConfig{Enabled: false},
			wantErr: false,
		},
		{
			name:      "enabled=true without column errors",
			tenancy:   &config.TenancyConfig{Enabled: true},
			wantErr:   true,
			wantInErr: "tenancy.column",
		},
		{
			name:    "enabled=true with column is valid",
			tenancy: &config.TenancyConfig{Enabled: true, Column: "workspace_id"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Tenancy = tt.tenancy

			_, err := config.ValidatePreParse(cfg)
			if tt.wantErr && err == nil {
				t.Fatalf("ValidatePreParse() error = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidatePreParse() unexpected error: %v", err)
			}
			if tt.wantErr && !strings.Contains(err.Error(), tt.wantInErr) {
				t.Errorf("ValidatePreParse() error = %v, want error mentioning %q", err, tt.wantInErr)
			}
		})
	}
}

func TestValidatePreParse_TenancyTypeRequiresImport(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(*config.RootConfig)
		wantErr   bool
		wantInErr string
	}{
		{
			name: "global type with import is valid",
			setup: func(cfg *config.RootConfig) {
				cfg.Tenancy = &config.TenancyConfig{
					Enabled: true,
					Column:  "workspace_id",
					Type:    &config.TypeOverride{Type: "uuid.UUID", Import: "github.com/google/uuid"},
				}
			},
			wantErr: false,
		},
		{
			name: "global type without import errors",
			setup: func(cfg *config.RootConfig) {
				cfg.Tenancy = &config.TenancyConfig{
					Enabled: true,
					Column:  "workspace_id",
					Type:    &config.TypeOverride{Type: "uuid.UUID"},
				}
			},
			wantErr:   true,
			wantInErr: "tenancy.type",
		},
		{
			name: "per-table type without import errors",
			setup: func(cfg *config.RootConfig) {
				cfg.Tenancy = &config.TenancyConfig{Enabled: true, Column: "workspace_id"}
				cfg.Tables["products"] = config.TableConfig{
					Tenancy: &config.TableTenancyConfig{
						Type: &config.TypeOverride{Type: "ids.WorkspaceID"},
					},
				}
			},
			wantErr:   true,
			wantInErr: "tables.products.tenancy.type",
		},
		{
			name: "per-table type with import is valid",
			setup: func(cfg *config.RootConfig) {
				cfg.Tenancy = &config.TenancyConfig{Enabled: true, Column: "workspace_id"}
				cfg.Tables["products"] = config.TableConfig{
					Tenancy: &config.TableTenancyConfig{
						Type: &config.TypeOverride{Type: "ids.WorkspaceID", Import: "myapp/pkg/ids"},
					},
				}
			},
			wantErr: false,
		},
		{
			// Views take the same TableTenancyConfig shape (PRD §4.9, §29.2.5),
			// so the same rule has to reach them — an unimportable type would
			// otherwise surface as a broken TenantResolver[T] at compile time.
			name: "per-view type without import errors",
			setup: func(cfg *config.RootConfig) {
				cfg.Tenancy = &config.TenancyConfig{Enabled: true, Column: "workspace_id"}
				cfg.Views["project_stats"] = config.ViewConfig{
					StructName: "ProjectStat",
					SQL:        "./views/project_stats.sql",
					Tenancy: &config.TableTenancyConfig{
						Type: &config.TypeOverride{Type: "ids.WorkspaceID"},
					},
				}
			},
			wantErr:   true,
			wantInErr: "views.project_stats.tenancy.type",
		},
		{
			name: "per-view type with import is valid",
			setup: func(cfg *config.RootConfig) {
				cfg.Tenancy = &config.TenancyConfig{Enabled: true, Column: "workspace_id"}
				cfg.Views["project_stats"] = config.ViewConfig{
					StructName: "ProjectStat",
					SQL:        "./views/project_stats.sql",
					Tenancy: &config.TableTenancyConfig{
						Type: &config.TypeOverride{Type: "ids.WorkspaceID", Import: "myapp/pkg/ids"},
					},
				}
			},
			wantErr: false,
		},
		{
			name: "empty type is valid (no import required when type is unset)",
			setup: func(cfg *config.RootConfig) {
				cfg.Tenancy = &config.TenancyConfig{
					Enabled: true,
					Column:  "workspace_id",
					Type:    &config.TypeOverride{},
				}
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.setup(cfg)

			_, err := config.ValidatePreParse(cfg)
			if tt.wantErr && err == nil {
				t.Fatalf("ValidatePreParse() error = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidatePreParse() unexpected error: %v", err)
			}
			if tt.wantErr && !strings.Contains(err.Error(), tt.wantInErr) {
				t.Errorf("ValidatePreParse() error = %v, want error mentioning %q", err, tt.wantInErr)
			}
			if tt.wantErr && !strings.Contains(err.Error(), "import") {
				t.Errorf("ValidatePreParse() error = %v, want error mentioning %q", err, "import")
			}
		})
	}
}

func TestLoadConfig_TenancyRoundTrip(t *testing.T) {
	path := writeTestConfig(t, `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models
tenancy:
  enabled: true
  column: workspace_id
  type:
    type: uuid.UUID
    import: github.com/google/uuid
tables:
  audit_logs:
    tenancy:
      enabled: false
  legacy_widgets:
    tenancy:
      column: org_id
`)

	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}
	if cfg.Tenancy == nil {
		t.Fatal("Tenancy should not be nil")
	}
	if !cfg.Tenancy.Enabled {
		t.Error("Tenancy.Enabled = false, want true")
	}
	if cfg.Tenancy.Column != "workspace_id" {
		t.Errorf("Tenancy.Column = %q, want %q", cfg.Tenancy.Column, "workspace_id")
	}
	if cfg.Tenancy.Required == nil {
		t.Fatal("Tenancy.Required should be defaulted when tenancy block is present")
	}
	if !*cfg.Tenancy.Required {
		t.Error("Tenancy.Required = false, want true (default)")
	}
	if cfg.Tenancy.Type == nil {
		t.Fatal("Tenancy.Type should not be nil")
	}
	if cfg.Tenancy.Type.Type != "uuid.UUID" {
		t.Errorf("Tenancy.Type.Type = %q, want %q", cfg.Tenancy.Type.Type, "uuid.UUID")
	}

	// Per-table opt-out round-trips correctly.
	audit := cfg.Tables["audit_logs"]
	if audit.Tenancy == nil || audit.Tenancy.Enabled == nil || *audit.Tenancy.Enabled {
		t.Errorf("audit_logs.Tenancy.Enabled = %+v, want false", audit.Tenancy)
	}
	if got := config.ResolveTableTenancyEnabled(audit, cfg.Tenancy); got {
		t.Errorf("ResolveTableTenancyEnabled(audit_logs) = true, want false (opt-out)")
	}

	// Per-table column override round-trips correctly.
	legacy := cfg.Tables["legacy_widgets"]
	if got := config.ResolveTableTenancyColumn(legacy, cfg.Tenancy); got != "org_id" {
		t.Errorf("ResolveTableTenancyColumn(legacy_widgets) = %q, want %q", got, "org_id")
	}
	// Tenanted tables without a column override fall through to global.
	other := cfg.Tables["products"] // absent key → zero-value TableConfig
	if got := config.ResolveTableTenancyColumn(other, cfg.Tenancy); got != "workspace_id" {
		t.Errorf("ResolveTableTenancyColumn(products) = %q, want %q", got, "workspace_id")
	}
}

func TestLoadConfig_TenancyRequiredExplicitFalse(t *testing.T) {
	path := writeTestConfig(t, `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models
tenancy:
  enabled: true
  column: workspace_id
  required: false
`)

	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}
	if cfg.Tenancy.Required == nil {
		t.Fatal("Tenancy.Required should be set")
	}
	if *cfg.Tenancy.Required {
		t.Error("Tenancy.Required = true, want false (explicit override)")
	}
}

// --- View tenancy (PRD §4.9 / §29.2.5) ---
//
// Views resolve tenancy through their own accessors. The precedence is
// identical to the table one — per-view override → global → default — so the
// cases below mirror the table tables above; the divergences live in the
// generator (nullability handling, no write path), not in resolution.

func TestResolveViewTenancyEnabled(t *testing.T) {
	tests := []struct {
		name   string
		view   config.ViewConfig
		global *config.TenancyConfig
		want   bool
	}{
		{
			name:   "no global no view is disabled",
			view:   config.ViewConfig{},
			global: nil,
			want:   false,
		},
		{
			name:   "global enabled with no per-view block inherits",
			view:   config.ViewConfig{},
			global: &config.TenancyConfig{Enabled: true, Column: "workspace_id"},
			want:   true,
		},
		{
			name: "per-view enabled false opts out of an enabled global",
			view: config.ViewConfig{
				Tenancy: &config.TableTenancyConfig{Enabled: new(false)},
			},
			global: &config.TenancyConfig{Enabled: true, Column: "workspace_id"},
			want:   false,
		},
		{
			name: "nil per-view enabled inherits global",
			view: config.ViewConfig{
				Tenancy: &config.TableTenancyConfig{Column: new("org_id")},
			},
			global: &config.TenancyConfig{Enabled: true, Column: "workspace_id"},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := config.ResolveViewTenancyEnabled(tt.view, tt.global)
			if got != tt.want {
				t.Errorf("ResolveViewTenancyEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveViewTenancyColumn(t *testing.T) {
	tests := []struct {
		name   string
		view   config.ViewConfig
		global *config.TenancyConfig
		want   string
	}{
		{
			name:   "no global no view resolves to empty",
			view:   config.ViewConfig{},
			global: nil,
			want:   "",
		},
		{
			name:   "global column with no per-view block",
			view:   config.ViewConfig{},
			global: &config.TenancyConfig{Enabled: true, Column: "workspace_id"},
			want:   "workspace_id",
		},
		{
			name: "per-view column beats global",
			view: config.ViewConfig{
				Tenancy: &config.TableTenancyConfig{Column: new("org_id")},
			},
			global: &config.TenancyConfig{Enabled: true, Column: "workspace_id"},
			want:   "org_id",
		},
		{
			name: "nil per-view column inherits global",
			view: config.ViewConfig{
				Tenancy: &config.TableTenancyConfig{Required: new(false)},
			},
			global: &config.TenancyConfig{Enabled: true, Column: "workspace_id"},
			want:   "workspace_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := config.ResolveViewTenancyColumn(tt.view, tt.global)
			if got != tt.want {
				t.Errorf("ResolveViewTenancyColumn() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveViewTenancyRequired(t *testing.T) {
	tests := []struct {
		name   string
		view   config.ViewConfig
		global *config.TenancyConfig
		want   bool
	}{
		{
			name:   "no global no view defaults to true (safe default)",
			view:   config.ViewConfig{},
			global: nil,
			want:   true,
		},
		{
			name:   "global required=false with no per-view block",
			view:   config.ViewConfig{},
			global: &config.TenancyConfig{Enabled: true, Column: "workspace_id", Required: new(false)},
			want:   false,
		},
		{
			name: "per-view required=false beats global required=true",
			view: config.ViewConfig{
				Tenancy: &config.TableTenancyConfig{Required: new(false)},
			},
			global: &config.TenancyConfig{Enabled: true, Column: "workspace_id", Required: new(true)},
			want:   false,
		},
		{
			name: "per-view required=true beats global required=false",
			view: config.ViewConfig{
				Tenancy: &config.TableTenancyConfig{Required: new(true)},
			},
			global: &config.TenancyConfig{Enabled: true, Column: "workspace_id", Required: new(false)},
			want:   true,
		},
		{
			name: "nil per-view required inherits global",
			view: config.ViewConfig{
				Tenancy: &config.TableTenancyConfig{Required: nil},
			},
			global: &config.TenancyConfig{Enabled: true, Column: "workspace_id", Required: new(true)},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := config.ResolveViewTenancyRequired(tt.view, tt.global)
			if got != tt.want {
				t.Errorf("ResolveViewTenancyRequired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveViewTenancyType(t *testing.T) {
	viewType := &config.TypeOverride{Type: "WorkspaceID", Import: "example.com/ids"}
	globalType := &config.TypeOverride{Type: "uuid.UUID", Import: "github.com/google/uuid"}

	tests := []struct {
		name   string
		view   config.ViewConfig
		global *config.TenancyConfig
		want   *config.TypeOverride
	}{
		{
			name:   "no override anywhere falls through to detection",
			view:   config.ViewConfig{},
			global: &config.TenancyConfig{Enabled: true, Column: "workspace_id"},
			want:   nil,
		},
		{
			name:   "global type with no per-view block",
			view:   config.ViewConfig{},
			global: &config.TenancyConfig{Enabled: true, Column: "workspace_id", Type: globalType},
			want:   globalType,
		},
		{
			name: "per-view type beats global",
			view: config.ViewConfig{
				Tenancy: &config.TableTenancyConfig{Type: viewType},
			},
			global: &config.TenancyConfig{Enabled: true, Column: "workspace_id", Type: globalType},
			want:   viewType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := config.ResolveViewTenancyType(tt.view, tt.global)
			if got != tt.want {
				t.Errorf("ResolveViewTenancyType() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// The per-view block must survive YAML round-tripping with tri-state pointers
// intact — an unset field has to stay nil so it inherits, rather than
// collapsing to the zero value and silently opting the view out.
func TestLoadConfig_ViewTenancyBlock(t *testing.T) {
	path := writeTestConfig(t, `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models
tenancy:
  enabled: true
  column: workspace_id
views:
  project_stats:
    struct_name: ProjectStat
    sql: "./views/project_stats.sql"
    tenancy:
      enabled: false
  legacy_rollup:
    struct_name: LegacyRollup
    sql: "./views/legacy_rollup.sql"
    tenancy:
      column: org_id
      required: false
      type:
        type: WorkspaceID
        import: example.com/ids
  plain_view:
    struct_name: PlainView
    sql: "./views/plain_view.sql"
`)

	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}

	stats := cfg.Views["project_stats"]
	if stats.Tenancy == nil || stats.Tenancy.Enabled == nil || *stats.Tenancy.Enabled {
		t.Errorf("views.project_stats.tenancy.enabled = %+v, want explicit false", stats.Tenancy)
	}
	if config.ResolveViewTenancyEnabled(stats, cfg.Tenancy) {
		t.Error("ResolveViewTenancyEnabled(project_stats) = true, want false (opt-out)")
	}

	rollup := cfg.Views["legacy_rollup"]
	if rollup.Tenancy == nil {
		t.Fatal("views.legacy_rollup.tenancy should not be nil")
	}
	if rollup.Tenancy.Enabled != nil {
		t.Errorf("views.legacy_rollup.tenancy.enabled = %v, want nil (inherit)", *rollup.Tenancy.Enabled)
	}
	if got := config.ResolveViewTenancyColumn(rollup, cfg.Tenancy); got != "org_id" {
		t.Errorf("ResolveViewTenancyColumn(legacy_rollup) = %q, want %q", got, "org_id")
	}
	if config.ResolveViewTenancyRequired(rollup, cfg.Tenancy) {
		t.Error("ResolveViewTenancyRequired(legacy_rollup) = true, want false")
	}
	gotType := config.ResolveViewTenancyType(rollup, cfg.Tenancy)
	if gotType == nil || gotType.Type != "WorkspaceID" || gotType.Import != "example.com/ids" {
		t.Errorf("ResolveViewTenancyType(legacy_rollup) = %+v, want WorkspaceID/example.com/ids", gotType)
	}

	plain := cfg.Views["plain_view"]
	if plain.Tenancy != nil {
		t.Errorf("views.plain_view.tenancy = %+v, want nil when the block is absent", plain.Tenancy)
	}
	if got := config.ResolveViewTenancyColumn(plain, cfg.Tenancy); got != "workspace_id" {
		t.Errorf("ResolveViewTenancyColumn(plain_view) = %q, want the inherited %q", got, "workspace_id")
	}
}
