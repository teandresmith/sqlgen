package gen_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// View-tenancy detection, attachment, and client wiring (PRD §29.2.3 rules 1–5
// extended to views, §29.2.4 uniform-type participation, §29.2.5 read-path-only
// semantics and the nullable warn-and-scope divergence).

// viewCol is the view-side twin of tableCol: a compact parser.Column builder
// for the fixtures below. Views carry no PK flag from DDL — the @pk annotation
// sets it — so the PK argument the table builder takes is absent here.
func viewCol(name, sqlType string, nullable bool) parser.Column {
	return parser.Column{Name: name, Type: sqlType, Nullable: nullable}
}

// viewSchema wraps one view's columns in a parser.Schema. Every fixture in
// this file is single-view unless it needs a table to diverge against.
func viewSchema(name string, cols ...parser.Column) *parser.Schema {
	return &parser.Schema{
		Views: []parser.View{{Name: name, Schema: "public", Columns: cols}},
	}
}

func TestBuildTenancyContext_viewDetection(t *testing.T) {
	tests := []struct {
		name       string
		viewCfg    *config.ViewConfig
		columns    []parser.Column
		want       gen.TenancyContext
		wantErr    string
		wantWarned bool
	}{
		{
			name: "tenant column present detects tenanted (rule 2)",
			columns: []parser.Column{
				viewCol("id", "uuid", false),
				viewCol("workspace_id", "uuid", false),
			},
			want: gen.TenancyContext{
				Tenanted: true,
				Column:   "workspace_id",
				Required: true,
				GoType:   "uuid.UUID",
				Import:   "github.com/google/uuid",
			},
		},
		{
			name: "tenant column absent detects shared (rule 3)",
			columns: []parser.Column{
				viewCol("id", "uuid", false),
				viewCol("total", "bigint", false),
			},
			want: gen.TenancyContext{Tenanted: false},
		},
		{
			name:    "enabled false forces shared even with the column present (rule 4)",
			viewCfg: &config.ViewConfig{Tenancy: &config.TableTenancyConfig{Enabled: new(false)}},
			columns: []parser.Column{
				viewCol("id", "uuid", false),
				viewCol("workspace_id", "uuid", false),
			},
			want: gen.TenancyContext{Tenanted: false},
		},
		{
			name:    "enabled true with the column absent is a hard error (rule 5)",
			viewCfg: &config.ViewConfig{Tenancy: &config.TableTenancyConfig{Enabled: new(true)}},
			columns: []parser.Column{
				viewCol("id", "uuid", false),
			},
			wantErr: `view public.project_stats declares tenancy.enabled=true but tenant column "workspace_id" is not present`,
		},
		{
			name:    "per-view column override names a non-standard column",
			viewCfg: &config.ViewConfig{Tenancy: &config.TableTenancyConfig{Column: new("org_id")}},
			columns: []parser.Column{
				viewCol("id", "uuid", false),
				viewCol("org_id", "uuid", false),
				viewCol("workspace_id", "uuid", false),
			},
			want: gen.TenancyContext{
				Tenanted: true,
				Column:   "org_id",
				Required: true,
				GoType:   "uuid.UUID",
				Import:   "github.com/google/uuid",
			},
		},
		{
			name:    "per-view required override beats the global default",
			viewCfg: &config.ViewConfig{Tenancy: &config.TableTenancyConfig{Required: new(false)}},
			columns: []parser.Column{
				viewCol("id", "uuid", false),
				viewCol("workspace_id", "uuid", false),
			},
			want: gen.TenancyContext{
				Tenanted: true,
				Column:   "workspace_id",
				Required: false,
				GoType:   "uuid.UUID",
				Import:   "github.com/google/uuid",
			},
		},
		{
			name: "per-view type override wins over schema resolution",
			viewCfg: &config.ViewConfig{Tenancy: &config.TableTenancyConfig{
				Type: &config.TypeOverride{Type: "WorkspaceID", Import: "example.com/ids"},
			}},
			columns: []parser.Column{
				viewCol("id", "uuid", false),
				viewCol("workspace_id", "uuid", false),
			},
			want: gen.TenancyContext{
				Tenanted: true,
				Column:   "workspace_id",
				Required: true,
				GoType:   "WorkspaceID",
				Import:   "example.com/ids",
			},
		},
		{
			name: "nullable tenant column warns and still scopes",
			columns: []parser.Column{
				viewCol("id", "uuid", false),
				viewCol("workspace_id", "uuid", true),
			},
			want: gen.TenancyContext{
				Tenanted: true,
				Column:   "workspace_id",
				Required: true,
				// The row struct spells the column nullable; the tenant value
				// bound into the predicate is always a plain T.
				GoType: "uuid.UUID",
				Import: "github.com/google/uuid",
			},
			wantWarned: true,
		},
		{
			name: "per-view type override mismatched against the SQL type errors",
			viewCfg: &config.ViewConfig{Tenancy: &config.TableTenancyConfig{
				Type: &config.TypeOverride{Type: "WorkspaceID", Import: "example.com/ids/uuid"},
			}},
			columns: []parser.Column{
				viewCol("id", "uuid", false),
				// bigint column — but the override's import screams "uuid".
				viewCol("workspace_id", "bigint", false),
			},
			wantErr: `view public.project_stats tenant column "workspace_id": tenancy.type WorkspaceID`,
		},
		{
			name:    "nullable tenant column with explicit enabled true hard-errors",
			viewCfg: &config.ViewConfig{Tenancy: &config.TableTenancyConfig{Enabled: new(true)}},
			columns: []parser.Column{
				viewCol("id", "uuid", false),
				viewCol("workspace_id", "uuid", true),
			},
			wantErr: `view public.project_stats declares tenancy.enabled=true but tenant column "workspace_id" is nullable`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tenancyTestConfig()
			cfg.Overrides.Types["uuid"] = config.TypeOverride{
				Type:   "uuid.UUID",
				Import: "github.com/google/uuid",
			}
			cfg.Views = make(map[string]config.ViewConfig)
			if tt.viewCfg != nil {
				cfg.Views["project_stats"] = *tt.viewCfg
			}
			schema := viewSchema("project_stats", tt.columns...)

			got, warnings, err := gen.BuildTenancyContext(cfg, schema, tenancyTestResolver(cfg))

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("BuildTenancyContext() err = nil, want error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("BuildTenancyContext() err = %q, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("BuildTenancyContext() err = %v, want nil", err)
			}

			entry, ok := got["public.project_stats"]
			if !ok {
				t.Fatalf("BuildTenancyContext() has no entry for public.project_stats; got keys %v", mapKeys(got))
			}
			if entry != tt.want {
				t.Errorf("BuildTenancyContext()[public.project_stats] = %+v, want %+v", entry, tt.want)
			}

			warned := len(warnings) > 0
			if warned != tt.wantWarned {
				t.Errorf("BuildTenancyContext() warnings = %v, want warned = %v", warnings, tt.wantWarned)
			}
			if tt.wantWarned && !strings.Contains(strings.Join(warnings, "\n"), "public.project_stats") {
				t.Errorf("nullable-column warning %v does not name the view", warnings)
			}
		})
	}
}

func mapKeys(m map[string]gen.TenancyContext) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// A view whose tenant column resolves to a different Go type than the tables'
// has no resolver to draw from — the package exposes exactly one concretely
// typed TenantResolver[T] (PRD §29.2.4, §29.2.5). The view must therefore
// appear in the same aggregated offender list a mismatched table produces.
func TestBuildTenancyContext_viewJoinsUniformTypeError(t *testing.T) {
	cfg := tenancyTestConfig()
	cfg.Overrides.Types["uuid"] = config.TypeOverride{
		Type:   "uuid.UUID",
		Import: "github.com/google/uuid",
	}
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "users",
				Schema: "public",
				Columns: []parser.Column{
					tableCol("id", "uuid", false, true),
					tableCol("workspace_id", "uuid", false, false),
				},
			},
		},
		Views: []parser.View{
			{
				Name:   "legacy_rollup",
				Schema: "public",
				Columns: []parser.Column{
					viewCol("id", "bigint", false),
					viewCol("workspace_id", "bigint", false),
				},
			},
		},
	}

	_, _, err := gen.BuildTenancyContext(cfg, schema, tenancyTestResolver(cfg))
	if err == nil {
		t.Fatalf("BuildTenancyContext() err = nil, want uniform-type error")
	}
	msg := err.Error()
	for _, want := range []string{"public.users", "view public.legacy_rollup", "uuid.UUID", "int64"} {
		if !strings.Contains(msg, want) {
			t.Errorf("uniform-type error %q missing %q", msg, want)
		}
	}
	if !strings.Contains(msg, "views.<name>.tenancy.enabled: false") {
		t.Errorf("uniform-type error %q does not offer the per-view opt-out remedy", msg)
	}
	if count := strings.Count(msg, "tenanted entities must resolve to a uniform tenant Go type"); count != 1 {
		t.Errorf("uniform-type error repeated %d times, want 1", count)
	}
}

// A view's @type annotation literal must reach the tenancy context, or the row
// struct and the tenant predicate would name two different Go types for one
// column — the view-side shape of the per-column type-override defect.
func TestBuildTenancyContext_viewGoTypeLiteralReachesTenancy(t *testing.T) {
	cfg := tenancyTestConfig()
	schema := viewSchema(
		"project_stats",
		viewCol("id", "uuid", false),
		parser.Column{
			Name:          "workspace_id",
			Type:          "uuid",
			GoTypeLiteral: "WorkspaceID",
			GoTypeImport:  "example.com/ids",
		},
	)

	got, _, err := gen.BuildTenancyContext(cfg, schema, tenancyTestResolver(cfg))
	if err != nil {
		t.Fatalf("BuildTenancyContext() err = %v, want nil", err)
	}
	entry := got["public.project_stats"]
	if entry.GoType != "WorkspaceID" || entry.Import != "example.com/ids" {
		t.Errorf("view tenant type = (%q, %q), want (WorkspaceID, example.com/ids)", entry.GoType, entry.Import)
	}
}

// Detection covers views on the read path only, so nothing about a view's
// tenancy entry depends on it being materialized. Pinned because §29.2.5 makes
// Refresh / RefreshConcurrently explicitly unscoped: a future contributor must
// not reach for a "matviews are different" branch in detection.
func TestBuildTenancyContext_materializedViewDetectsIdentically(t *testing.T) {
	cfg := tenancyTestConfig()
	schema := &parser.Schema{
		Views: []parser.View{
			{
				Name:                    "project_stats",
				Schema:                  "public",
				Materialized:            true,
				ConcurrentlyRefreshable: true,
				Columns: []parser.Column{
					viewCol("id", "uuid", false),
					viewCol("workspace_id", "uuid", false),
				},
			},
		},
	}

	got, _, err := gen.BuildTenancyContext(cfg, schema, tenancyTestResolver(cfg))
	if err != nil {
		t.Fatalf("BuildTenancyContext() err = %v, want nil", err)
	}
	if entry := got["public.project_stats"]; !entry.Tenanted || entry.Column != "workspace_id" {
		t.Errorf("matview tenancy entry = %+v, want tenanted on workspace_id", entry)
	}
}

func TestAttachTenancyToViews(t *testing.T) {
	views := []gen.ViewContext{
		{
			StructName: "ProjectStat",
			ViewName:   "project_stats",
			Schema:     "public",
			Columns: []gen.ColumnContext{
				{Name: "workspace_id", FieldName: "WorkspaceID"},
			},
			Imports: []string{"context"},
		},
		{
			StructName: "GlobalRollup",
			ViewName:   "global_rollups",
			Schema:     "public",
			Imports:    []string{"context"},
		},
	}
	tenancyMap := map[string]gen.TenancyContext{
		"public.project_stats": {
			Tenanted: true,
			Column:   "workspace_id",
			Required: true,
			GoType:   "uuid.UUID",
			Import:   "github.com/google/uuid",
		},
		"public.global_rollups": {Tenanted: false},
	}

	gen.AttachTenancyToViewsForTest(views, tenancyMap)

	tenanted := views[0].Tenancy
	if tenanted == nil {
		t.Fatalf("tenanted view Tenancy = nil, want populated context")
	}
	want := gen.TableTenancyContext{
		Tenanted:  true,
		Column:    "workspace_id",
		FieldName: "WorkspaceID",
		GoType:    "uuid.UUID",
		Import:    "github.com/google/uuid",
		Required:  true,
	}
	if *tenanted != want {
		t.Errorf("tenanted view Tenancy = %+v, want %+v", *tenanted, want)
	}
	// A view has no DDL primary key, so §29.7's composite-PK rule never
	// applies to one (§29.2.5).
	if tenanted.InPrimaryKey {
		t.Errorf("view Tenancy.InPrimaryKey = true, want false — views have no DDL PK")
	}
	for _, imp := range []string{"github.com/teandresmith/sqlgen/tenancy", "github.com/google/uuid", "context"} {
		if !slices.Contains(views[0].Imports, imp) {
			t.Errorf("tenanted view Imports = %v, missing %q", views[0].Imports, imp)
		}
	}

	shared := views[1].Tenancy
	if shared == nil || shared.Tenanted {
		t.Errorf("shared view Tenancy = %+v, want non-nil and not tenanted", shared)
	}
	if slices.Contains(views[1].Imports, "github.com/teandresmith/sqlgen/tenancy") {
		t.Errorf("shared view Imports = %v, must not carry the tenancy import", views[1].Imports)
	}
}

// Tenancy globally disabled means BuildTenancyContext returns a nil map, and a
// nil map must leave every ViewContext.Tenancy nil so the view templates emit
// exactly the code they emitted before views gained tenancy.
func TestAttachTenancyToViews_nilMapLeavesTenancyNil(t *testing.T) {
	cfg := tenancyTestConfig()
	cfg.Tenancy.Enabled = false
	schema := viewSchema(
		"project_stats",
		viewCol("id", "uuid", false),
		viewCol("workspace_id", "uuid", false),
	)

	tenancyMap, warnings, err := gen.BuildTenancyContext(cfg, schema, tenancyTestResolver(cfg))
	if err != nil {
		t.Fatalf("BuildTenancyContext() err = %v, want nil", err)
	}
	if tenancyMap != nil {
		t.Fatalf("BuildTenancyContext() = %v, want nil map when tenancy disabled", tenancyMap)
	}
	if len(warnings) != 0 {
		t.Errorf("BuildTenancyContext() warnings = %v, want none when tenancy disabled", warnings)
	}

	views := []gen.ViewContext{{StructName: "ProjectStat", ViewName: "project_stats", Schema: "public"}}
	gen.AttachTenancyToViewsForTest(views, tenancyMap)
	if views[0].Tenancy != nil {
		t.Errorf("ViewContext.Tenancy = %+v, want nil when tenancy is globally disabled", views[0].Tenancy)
	}
}

// The unified client threads options.tenantResolver into every entity named by
// TenantedEntities. A tenanted view's read methods need the resolver for the
// same reason a table's do (PRD §29.2.5), so the view must be in that list.
func TestBuildClientContext_tenantedEntitiesIncludeViews(t *testing.T) {
	cfg := &config.RootConfig{
		Tenancy: &config.TenancyConfig{Enabled: true, Column: "workspace_id", Required: new(true)},
	}
	tables := []gen.TableContext{
		{
			StructName: "Product",
			Tenancy: &gen.TableTenancyContext{
				Tenanted: true, Column: "workspace_id",
				GoType: "uuid.UUID", Import: "github.com/google/uuid",
			},
		},
	}
	views := []gen.ViewContext{
		{
			StructName: "ProjectStat",
			Tenancy: &gen.TableTenancyContext{
				Tenanted: true, Column: "workspace_id",
				GoType: "uuid.UUID", Import: "github.com/google/uuid",
			},
		},
		{
			StructName: "GlobalRollup",
			Tenancy:    &gen.TableTenancyContext{Tenanted: false},
		},
	}
	tenancyMap := map[string]gen.TenancyContext{
		"public.products": {
			Tenanted: true, Column: "workspace_id",
			GoType: "uuid.UUID", Import: "github.com/google/uuid",
		},
		"public.project_stats":  {Tenanted: true, Column: "workspace_id", GoType: "uuid.UUID", Import: "github.com/google/uuid"},
		"public.global_rollups": {Tenanted: false},
	}

	ctx := gen.BuildClientContext(tables, views, "db", "Client", false, nil, cfg, tenancyMap)

	want := []string{"product", "projectStat"}
	if !slices.Equal(ctx.TenantedEntities, want) {
		t.Errorf("TenantedEntities = %v, want %v", ctx.TenantedEntities, want)
	}
}

// End-to-end through Generate: the orchestrator must call attachTenancyToViews,
// and the read-path emission must land in the view's own generated file —
// the tenant predicate in GetMany and Count, the resolver field and
// resolveTenant on the view client — while the tenancy-disabled generation of
// the same schema stays free of all of it.
func TestGenerate_tenantedViewEmitsReadPathScoping(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "products",
				Schema: "public",
				Columns: []parser.Column{
					tableCol("id", "uuid", false, true),
					tableCol("workspace_id", "uuid", false, false),
				},
			},
		},
		Views: []parser.View{
			{
				Name:   "project_stats",
				Schema: "public",
				Columns: []parser.Column{
					viewCol("id", "uuid", false),
					viewCol("workspace_id", "uuid", false),
					viewCol("total", "bigint", false),
				},
			},
		},
	}

	tenantedDir := t.TempDir()
	tenantedCfg := viewTenancyGenerateConfig(t, tenantedDir, true)
	tenantedResult, err := gen.Generate(schema, tenantedCfg, "test")
	if err != nil {
		t.Fatalf("Generate() with tenancy enabled: %v", err)
	}
	if len(tenantedResult.Views) != 1 {
		t.Fatalf("Generate() produced %d view contexts, want 1", len(tenantedResult.Views))
	}
	vt := tenantedResult.Views[0].Tenancy
	if vt == nil || !vt.Tenanted || vt.Column != "workspace_id" {
		t.Fatalf("view Tenancy = %+v, want tenanted on workspace_id", vt)
	}

	sharedDir := t.TempDir()
	sharedCfg := viewTenancyGenerateConfig(t, sharedDir, false)
	sharedResult, err := gen.Generate(schema, sharedCfg, "test")
	if err != nil {
		t.Fatalf("Generate() with tenancy disabled: %v", err)
	}
	if sharedResult.Views[0].Tenancy != nil {
		t.Errorf("view Tenancy = %+v, want nil when tenancy is globally disabled", sharedResult.Views[0].Tenancy)
	}

	tenantedView := readGenFile(t, tenantedDir, "project_stat_gen.go")
	sharedView := readGenFile(t, sharedDir, "project_stat_gen.go")

	for _, want := range []string{
		// The tenant column is `uuid` and this config declares no override,
		// so it resolves to the standard library's uuid.UUID (PRD §7.2).
		"tenantResolver tenancy.TenantResolver[uuid.UUID]",
		"func (c *projectStatClient) resolveTenant(ctx context.Context, explicit *uuid.UUID) (uuid.UUID, bool, error)",
		`conds = append(conds, sql.Where(c.dialect.QuoteIdentifier("workspace_id")).Eq(resolvedTenant))`,
		"if !options.SkipTenancy {",
	} {
		if !strings.Contains(tenantedView, want) {
			t.Errorf("tenanted view file is missing %q:\n%s", want, tenantedView)
		}
	}
	// GetMany and Count each inject once; Get, Paginate and Connection compose
	// them, so two occurrences is the whole read surface.
	if got := strings.Count(tenantedView, `sql.Where(c.dialect.QuoteIdentifier("workspace_id")).Eq(resolvedTenant)`); got != 2 {
		t.Errorf("tenant predicate appears %d times, want 2 (GetMany + Count)", got)
	}
	// The tenancy import is now referenced, so goimports keeps it.
	if !strings.Contains(tenantedView, `"github.com/teandresmith/sqlgen/tenancy"`) {
		t.Errorf("tenanted view file does not import the tenancy package:\n%s", tenantedView)
	}

	for _, unwanted := range []string{"resolveTenant", "tenantResolver", "SkipTenancy", "workspace_id\").Eq"} {
		if strings.Contains(sharedView, unwanted) {
			t.Errorf("tenancy-disabled view file emitted %q:\n%s", unwanted, sharedView)
		}
	}
}

func viewTenancyGenerateConfig(t *testing.T, outDir string, tenancyEnabled bool) *config.RootConfig {
	t.Helper()
	body := `version: v1
input:
  dialect: postgres
  paths:
    - ./schema.sql
output:
  driver: pgx
  dir: ` + outDir + `
  package: db
  layout: file_per_table
`
	if tenancyEnabled {
		body += `tenancy:
  enabled: true
  column: workspace_id
  required: true
`
	}
	cfgPath := filepath.Join(t.TempDir(), "sqlgen.yml")
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	return cfg
}

func readGenFile(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // test helper, reading back a t.TempDir the test just generated into
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return string(b)
}

// The nullable-tenant-column warning is only useful if it reaches a human. It
// is raised inside detection, so it has to be threaded out of
// BuildTenancyContext and onto GenerateResult.Warnings, which is what the CLI
// prints.
func TestGenerate_nullableViewTenantColumnWarningReachesResult(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "products",
				Schema: "public",
				Columns: []parser.Column{
					tableCol("id", "uuid", false, true),
					tableCol("workspace_id", "uuid", false, false),
				},
			},
		},
		Views: []parser.View{
			{
				Name:   "project_stats",
				Schema: "public",
				Columns: []parser.Column{
					viewCol("id", "uuid", false),
					// A LEFT JOIN in the view's SELECT widens a NOT NULL base
					// column — the legitimate case §29.2.5 refuses to error on.
					viewCol("workspace_id", "uuid", true),
					viewCol("total", "bigint", false),
				},
			},
		},
	}

	result, err := gen.Generate(schema, viewTenancyGenerateConfig(t, t.TempDir(), true), "test")
	if err != nil {
		t.Fatalf("Generate() err = %v, want a warning, not an error", err)
	}
	joined := strings.Join(result.Warnings, "\n")
	if !strings.Contains(joined, "public.project_stats") || !strings.Contains(joined, "nullable") {
		t.Errorf("GenerateResult.Warnings = %v, want the nullable-tenant-column notice naming the view", result.Warnings)
	}
	if v := result.Views[0].Tenancy; v == nil || !v.Tenanted {
		t.Errorf("view Tenancy = %+v, want still scoped despite the nullable column", v)
	}
}

// §29.2.5 promises `CallOptions` parity: SkipTenancy and the explicit Tenant
// field "already reach every view read method". They only do if the shared
// types context sees the view — in a package whose tables all opt out and
// whose views carry the tenant column, deriving the uniform tenant type from
// tables alone emits SkipTenancy without Tenant, and the view's ported
// resolveTenant(ctx, explicit) then has no field to read.
func TestGenerate_viewOnlyTenantedPackageStillEmitsExplicitTenantOption(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "products",
				Schema: "public",
				Columns: []parser.Column{
					tableCol("id", "uuid", false, true),
					tableCol("name", "text", false, false),
				},
			},
		},
		Views: []parser.View{
			{
				Name:   "project_stats",
				Schema: "public",
				Columns: []parser.Column{
					viewCol("id", "uuid", false),
					viewCol("workspace_id", "uuid", false),
				},
			},
		},
	}

	outDir := t.TempDir()
	if _, err := gen.Generate(schema, viewTenancyGenerateConfig(t, outDir, true), "test"); err != nil {
		t.Fatalf("Generate() err = %v", err)
	}

	shared := readGenFile(t, outDir, "shared_types_gen.go")
	if !strings.Contains(shared, "SkipTenancy") {
		t.Errorf("shared_types_gen.go missing SkipTenancy; got:\n%s", shared)
	}
	// The fixture config declares no `overrides.types.uuid`, so the tenant
	// column resolves to the default binding — `uuid.UUID` (PRD §7.2). What
	// matters is that the field exists and is concretely typed, not which
	// type it landed on. Matched loosely on the run of spaces, because gofumpt
	// re-aligns the whole struct whenever a longer field name is added to it.
	if !regexp.MustCompile(`Tenant +\*uuid\.UUID`).MatchString(shared) {
		t.Errorf("shared_types_gen.go missing the concretely-typed explicit-tenant field; got:\n%s", shared)
	}
}
