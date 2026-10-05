package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/parser"
)

// tenancyTestConfig returns a minimal RootConfig with tenancy globally
// enabled. Tests compose per-table overrides on top by mutating cfg.Tables.
func tenancyTestConfig() *config.RootConfig {
	cfg := &config.RootConfig{}
	cfg.Input.Dialect = config.DialectPostgres
	cfg.Tables = make(map[string]config.TableConfig)
	cfg.Extras = make(map[string]config.ExtraType)
	cfg.Overrides.Types = make(map[string]config.TypeOverride)
	cfg.Overrides.UsePointers = new(true)
	cfg.Tenancy = &config.TenancyConfig{
		Enabled:  true,
		Column:   "workspace_id",
		Required: new(true),
	}
	return cfg
}

func tenancyTestResolver(cfg *config.RootConfig) *gotype.Resolver {
	return gotype.NewResolver(cfg.Input.Dialect, true, cfg.Overrides.Types)
}

// tableCol is a compact builder for parser.Column used by the table-driven
// test fixtures below.
func tableCol(name, sqlType string, nullable, pk bool) parser.Column {
	return parser.Column{
		Name:       name,
		Type:       sqlType,
		Nullable:   nullable,
		PrimaryKey: pk,
	}
}

func TestBuildTenancyContext_disabledReturnsNil(t *testing.T) {
	cfg := tenancyTestConfig()
	cfg.Tenancy.Enabled = false
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
	}

	got, _, err := gen.BuildTenancyContext(cfg, schema, tenancyTestResolver(cfg))
	if err != nil {
		t.Fatalf("BuildTenancyContext() err = %v, want nil", err)
	}
	if got != nil {
		t.Fatalf("BuildTenancyContext() = %v, want nil when tenancy disabled", got)
	}
}

func TestBuildTenancyContext_mixedTenantedAndShared(t *testing.T) {
	cfg := tenancyTestConfig()
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "products",
				Schema: "public",
				Columns: []parser.Column{
					tableCol("id", "uuid", false, true),
					tableCol("workspace_id", "uuid", false, false),
					tableCol("name", "text", false, false),
				},
			},
			{
				Name:   "audit_logs",
				Schema: "public",
				Columns: []parser.Column{
					tableCol("id", "uuid", false, true),
					tableCol("message", "text", false, false),
				},
			},
		},
	}

	got, _, err := gen.BuildTenancyContext(cfg, schema, tenancyTestResolver(cfg))
	if err != nil {
		t.Fatalf("BuildTenancyContext() err = %v, want nil", err)
	}

	products, ok := got["public.products"]
	if !ok {
		t.Fatalf("missing entry for public.products")
	}
	if !products.Tenanted {
		t.Errorf("public.products.Tenanted = false, want true")
	}
	if products.Column != "workspace_id" {
		t.Errorf("public.products.Column = %q, want %q", products.Column, "workspace_id")
	}
	if products.GoType != "uuid.UUID" {
		// The tenant column is `uuid`, which resolves to the standard
		// library's uuid.UUID with no configuration (PRD §7.2) — that's
		// what the resolver returns and what this fixture exposes.
		t.Errorf("public.products.GoType = %q, want %q", products.GoType, "uuid.UUID")
	}
	if !products.Required {
		t.Errorf("public.products.Required = false, want true (inherited from global)")
	}

	audit, ok := got["public.audit_logs"]
	if !ok {
		t.Fatalf("missing entry for public.audit_logs")
	}
	if audit.Tenanted {
		t.Errorf("public.audit_logs.Tenanted = true, want false (column absent)")
	}
}

func TestBuildTenancyContext_forcedSharedOptOutWinsOverDetection(t *testing.T) {
	cfg := tenancyTestConfig()
	cfg.Tables["public.products"] = config.TableConfig{
		Tenancy: &config.TableTenancyConfig{Enabled: new(false)},
	}
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
	}

	got, _, err := gen.BuildTenancyContext(cfg, schema, tenancyTestResolver(cfg))
	if err != nil {
		t.Fatalf("BuildTenancyContext() err = %v, want nil", err)
	}
	entry := got["public.products"]
	if entry.Tenanted {
		t.Errorf("Tenanted = true, want false (per-table opt-out wins even when column exists)")
	}
}

func TestBuildTenancyContext_perTableColumnOverride(t *testing.T) {
	cfg := tenancyTestConfig()
	orgID := "org_id"
	cfg.Tables["legacy_widgets"] = config.TableConfig{
		Tenancy: &config.TableTenancyConfig{Column: &orgID},
	}
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "legacy_widgets",
				Columns: []parser.Column{
					tableCol("id", "int", false, true),
					tableCol("org_id", "uuid", false, false),
				},
			},
		},
	}

	got, _, err := gen.BuildTenancyContext(cfg, schema, tenancyTestResolver(cfg))
	if err != nil {
		t.Fatalf("BuildTenancyContext() err = %v, want nil", err)
	}
	entry := got["legacy_widgets"]
	if !entry.Tenanted {
		t.Fatalf("Tenanted = false, want true (per-table column override points at existing column)")
	}
	if entry.Column != "org_id" {
		t.Errorf("Column = %q, want %q", entry.Column, "org_id")
	}
}

func TestBuildTenancyContext_compositePKTenantInPK(t *testing.T) {
	cfg := tenancyTestConfig()
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "order_items",
				Columns: []parser.Column{
					tableCol("workspace_id", "uuid", false, true),
					tableCol("product_id", "uuid", false, true),
					tableCol("quantity", "int", false, false),
				},
			},
		},
	}

	got, _, err := gen.BuildTenancyContext(cfg, schema, tenancyTestResolver(cfg))
	if err != nil {
		t.Fatalf("BuildTenancyContext() err = %v, want nil", err)
	}
	entry := got["order_items"]
	if !entry.Tenanted {
		t.Errorf("Tenanted = false, want true (tenant column is part of composite PK, still detected)")
	}
	if entry.Column != "workspace_id" {
		t.Errorf("Column = %q, want %q", entry.Column, "workspace_id")
	}
}

func TestBuildTenancyContext_perTableEnabledTrueMissingColumnErrors(t *testing.T) {
	cfg := tenancyTestConfig()
	cfg.Tenancy.Enabled = false // global off so detection would not apply
	cfg.Tables["events"] = config.TableConfig{
		Tenancy: &config.TableTenancyConfig{Enabled: new(true)},
	}
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "events",
				Columns: []parser.Column{
					tableCol("id", "uuid", false, true),
				},
			},
		},
	}

	_, _, err := gen.BuildTenancyContext(cfg, schema, tenancyTestResolver(cfg))
	// Global disabled → function returns nil map and nil error without
	// walking tables, so the per-table check doesn't fire. Flip global to
	// true to exercise the missing-column error.
	if err != nil {
		t.Fatalf("BuildTenancyContext() with global disabled err = %v, want nil", err)
	}

	cfg.Tenancy.Enabled = true
	_, _, err = gen.BuildTenancyContext(cfg, schema, tenancyTestResolver(cfg))
	if err == nil {
		t.Fatalf("BuildTenancyContext() err = nil, want error for missing tenant column")
	}
	msg := err.Error()
	if !strings.Contains(msg, "events") {
		t.Errorf("error %q does not name offending table %q", msg, "events")
	}
	if !strings.Contains(msg, "workspace_id") {
		t.Errorf("error %q does not name expected column %q", msg, "workspace_id")
	}
}

func TestBuildTenancyContext_nullableTenantColumnErrors(t *testing.T) {
	cfg := tenancyTestConfig()
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "documents",
				Columns: []parser.Column{
					tableCol("id", "uuid", false, true),
					tableCol("workspace_id", "uuid", true, false), // NULLABLE
				},
			},
		},
	}

	_, _, err := gen.BuildTenancyContext(cfg, schema, tenancyTestResolver(cfg))
	if err == nil {
		t.Fatalf("BuildTenancyContext() err = nil, want nullable-column error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "documents") {
		t.Errorf("error %q does not name offending table", msg)
	}
	if !strings.Contains(msg, "workspace_id") {
		t.Errorf("error %q does not name offending column", msg)
	}
	if !strings.Contains(msg, "nullable") {
		t.Errorf("error %q does not mention nullability remediation", msg)
	}
}

func TestBuildTenancyContext_nonComparableTypeErrors(t *testing.T) {
	cfg := tenancyTestConfig()
	// Extra type with a slice field → non-comparable struct.
	cfg.Extras["BadTenant"] = config.ExtraType{
		Fields: map[string]config.ExtraTypeField{
			"parts": {Type: "[]string"},
		},
	}
	cfg.Tenancy.Type = &config.TypeOverride{
		Type:   "BadTenant",
		Import: "example.com/bad",
	}
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					tableCol("id", "uuid", false, true),
					tableCol("workspace_id", "uuid", false, false),
				},
			},
		},
	}

	_, _, err := gen.BuildTenancyContext(cfg, schema, tenancyTestResolver(cfg))
	if err == nil {
		t.Fatalf("BuildTenancyContext() err = nil, want non-comparable-type error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "products") {
		t.Errorf("error %q does not name offending table", msg)
	}
	if !strings.Contains(msg, "BadTenant") {
		t.Errorf("error %q does not name offending type", msg)
	}
	if !strings.Contains(msg, "comparable") {
		t.Errorf("error %q does not mention comparable remediation", msg)
	}
}

func TestBuildTenancyContext_mixedTypesAggregatedError(t *testing.T) {
	cfg := tenancyTestConfig()
	// users.workspace_id has a per-table override to uuid.UUID while
	// legacy_widgets keeps the resolver's natural int64 for bigint.
	cfg.Tables["users"] = config.TableConfig{
		Tenancy: &config.TableTenancyConfig{
			Type: &config.TypeOverride{
				Type:   "uuid.UUID",
				Import: "github.com/google/uuid",
			},
		},
	}
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "users",
				Columns: []parser.Column{
					tableCol("id", "uuid", false, true),
					tableCol("workspace_id", "uuid", false, false),
				},
			},
			{
				Name: "legacy_widgets",
				Columns: []parser.Column{
					tableCol("id", "bigint", false, true),
					tableCol("workspace_id", "bigint", false, false),
				},
			},
		},
	}

	_, _, err := gen.BuildTenancyContext(cfg, schema, tenancyTestResolver(cfg))
	if err == nil {
		t.Fatalf("BuildTenancyContext() err = nil, want uniform-type error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "users") {
		t.Errorf("uniform-type error %q missing offending table %q", msg, "users")
	}
	if !strings.Contains(msg, "legacy_widgets") {
		t.Errorf("uniform-type error %q missing offending table %q", msg, "legacy_widgets")
	}
	if !strings.Contains(msg, "uuid.UUID") {
		t.Errorf("uniform-type error %q missing type %q", msg, "uuid.UUID")
	}
	if !strings.Contains(msg, "int64") {
		t.Errorf("uniform-type error %q missing type %q", msg, "int64")
	}
	// Invariant: the uniform-type check emits ONCE per generate run, not
	// N errors — the caller fixes a drift-set, not N individual drifts.
	if count := strings.Count(msg, "tenanted entities must resolve to a uniform tenant Go type"); count != 1 {
		t.Errorf("uniform-type error repeated %d times, want 1", count)
	}
}

func TestBuildTenancyContext_overrideVsSQLTypeMismatchErrors(t *testing.T) {
	cfg := tenancyTestConfig()
	cfg.Tables["products"] = config.TableConfig{
		Tenancy: &config.TableTenancyConfig{
			Type: &config.TypeOverride{
				Type:   "WorkspaceID",
				Import: "example.com/ids/uuid",
			},
		},
	}
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					tableCol("id", "uuid", false, true),
					// bigint column — but the override's import screams
					// "uuid".
					tableCol("workspace_id", "bigint", false, false),
				},
			},
		},
	}

	_, _, err := gen.BuildTenancyContext(cfg, schema, tenancyTestResolver(cfg))
	if err == nil {
		t.Fatalf("BuildTenancyContext() err = nil, want override/SQL-type mismatch error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "products") {
		t.Errorf("override-mismatch error %q missing offending table", msg)
	}
	if !strings.Contains(msg, "WorkspaceID") {
		t.Errorf("override-mismatch error %q missing override type", msg)
	}
	if !strings.Contains(msg, "bigint") {
		t.Errorf("override-mismatch error %q missing SQL type", msg)
	}
}

func TestBuildTenancyContext_uniformUUIDAcceptsGlobalOverride(t *testing.T) {
	// Global tenancy.type override declares uuid.UUID; two tables with
	// uuid columns both use it → no divergence, no error.
	cfg := tenancyTestConfig()
	cfg.Tenancy.Type = &config.TypeOverride{
		Type:   "uuid.UUID",
		Import: "github.com/google/uuid",
	}
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					tableCol("id", "uuid", false, true),
					tableCol("workspace_id", "uuid", false, false),
				},
			},
			{
				Name: "orders",
				Columns: []parser.Column{
					tableCol("id", "uuid", false, true),
					tableCol("workspace_id", "uuid", false, false),
				},
			},
		},
	}

	got, _, err := gen.BuildTenancyContext(cfg, schema, tenancyTestResolver(cfg))
	if err != nil {
		t.Fatalf("BuildTenancyContext() err = %v, want nil", err)
	}
	for key, entry := range got {
		if !entry.Tenanted {
			t.Errorf("%s.Tenanted = false, want true", key)
		}
		if entry.GoType != "uuid.UUID" {
			t.Errorf("%s.GoType = %q, want %q", key, entry.GoType, "uuid.UUID")
		}
		if entry.Import != "github.com/google/uuid" {
			t.Errorf("%s.Import = %q, want %q", key, entry.Import, "github.com/google/uuid")
		}
	}
}

func TestBuildTenancyContext_requiredTriStateInherits(t *testing.T) {
	cfg := tenancyTestConfig()
	cfg.Tenancy.Required = new(false) // global: false
	cfg.Tables["strict_table"] = config.TableConfig{
		Tenancy: &config.TableTenancyConfig{Required: new(true)}, // per-table: true
	}
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "lenient_table",
				Columns: []parser.Column{
					tableCol("id", "uuid", false, true),
					tableCol("workspace_id", "uuid", false, false),
				},
			},
			{
				Name: "strict_table",
				Columns: []parser.Column{
					tableCol("id", "uuid", false, true),
					tableCol("workspace_id", "uuid", false, false),
				},
			},
		},
	}

	got, _, err := gen.BuildTenancyContext(cfg, schema, tenancyTestResolver(cfg))
	if err != nil {
		t.Fatalf("BuildTenancyContext() err = %v, want nil", err)
	}
	if got["lenient_table"].Required {
		t.Errorf("lenient_table.Required = true, want false (inherited from global)")
	}
	if !got["strict_table"].Required {
		t.Errorf("strict_table.Required = false, want true (per-table override)")
	}
}

// TestBuildTenancyContext_columnMapTypeOverride pins the tenancy half of type
// overrides: a `column_map.<col>.type` override on the tenant column reaches
// the tenancy context, not just the row struct. The two must agree — a tenant
// column typed one way on the struct and another on the resolver would not
// compile.
func TestBuildTenancyContext_columnMapTypeOverride(t *testing.T) {
	cfg := tenancyTestConfig()
	cfg.Tables["public.projects"] = config.TableConfig{
		ColumnMap: map[string]config.ColumnOverride{
			"workspace_id": {Type: "uuid.UUID", Import: "github.com/google/uuid"},
		},
	}
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "projects",
				Schema: "public",
				Columns: []parser.Column{
					tableCol("id", "uuid", false, true),
					tableCol("workspace_id", "uuid", false, false),
				},
			},
		},
	}

	got, _, err := gen.BuildTenancyContext(cfg, schema, tenancyTestResolver(cfg))
	if err != nil {
		t.Fatalf("BuildTenancyContext() err = %v, want nil", err)
	}
	projects, ok := got["public.projects"]
	if !ok {
		t.Fatalf("missing entry for public.projects")
	}
	if projects.GoType != "uuid.UUID" {
		t.Errorf("public.projects.GoType = %q, want %q", projects.GoType, "uuid.UUID")
	}
	if projects.Import != "github.com/google/uuid" {
		t.Errorf("public.projects.Import = %q, want %q", projects.Import, "github.com/google/uuid")
	}
}

// TestBuildTenancyContext_tenancyTypeBeatsColumnMap pins the §29.2.4
// ordering: the dedicated tenancy.type override outranks the column's own
// column_map entry, which sits at step 3 alongside type_map.
func TestBuildTenancyContext_tenancyTypeBeatsColumnMap(t *testing.T) {
	cfg := tenancyTestConfig()
	cfg.Tables["public.projects"] = config.TableConfig{
		Tenancy: &config.TableTenancyConfig{
			Type: &config.TypeOverride{Type: "WorkspaceID", Import: "example.com/ids"},
		},
		ColumnMap: map[string]config.ColumnOverride{
			"workspace_id": {Type: "uuid.UUID", Import: "github.com/google/uuid"},
		},
	}
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "projects",
				Schema: "public",
				Columns: []parser.Column{
					tableCol("id", "uuid", false, true),
					tableCol("workspace_id", "uuid", false, false),
				},
			},
		},
	}

	got, _, err := gen.BuildTenancyContext(cfg, schema, tenancyTestResolver(cfg))
	if err != nil {
		t.Fatalf("BuildTenancyContext() err = %v, want nil", err)
	}
	projects, ok := got["public.projects"]
	if !ok {
		t.Fatalf("missing entry for public.projects")
	}
	if projects.GoType != "WorkspaceID" {
		t.Errorf("public.projects.GoType = %q, want %q", projects.GoType, "WorkspaceID")
	}
}
