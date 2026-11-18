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

// withTenantAnchor appends a tenanted table to schema. A fixture whose only
// entity is shared needs one: a tenancy-enabled package with no tenanted
// entity has no type for its TenantResolver[T] and is refused.
func withTenantAnchor(schema *parser.Schema) *parser.Schema {
	schema.Tables = append(schema.Tables, parser.Table{
		Name:   "tenant_anchors",
		Schema: "public",
		Columns: []parser.Column{
			tableCol("id", "uuid", false, true),
			tableCol("workspace_id", "uuid", false, false),
		},
	})
	return schema
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
	schema := withTenantAnchor(&parser.Schema{
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
	})

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

// TestBuildTenancyContext_noTenantedEntityErrors pins the refusal of a
// tenancy-enabled package in which no table or view is tenanted: the
// package's TenantResolver[T] takes T from a tenanted entity's tenant column
// (§29.3.1), so there is no type to emit it with.
func TestBuildTenancyContext_noTenantedEntityErrors(t *testing.T) {
	products := parser.Table{
		Name:   "products",
		Schema: "public",
		Columns: []parser.Column{
			tableCol("id", "uuid", false, true),
			tableCol("workspace_id", "uuid", false, false),
		},
	}
	widgets := parser.Table{
		Name:   "widgets",
		Schema: "public",
		Columns: []parser.Column{
			tableCol("id", "uuid", false, true),
			tableCol("name", "text", false, false),
		},
	}
	stats := parser.View{
		Name:   "product_stats",
		Schema: "public",
		Columns: []parser.Column{
			viewCol("id", "uuid", false),
			viewCol("workspace_id", "uuid", false),
		},
	}
	const refusal = `no table or view is tenanted: no generated table or view carries tenant column "workspace_id"`

	tests := []struct {
		name       string
		schema     *parser.Schema
		tables     map[string]config.TableConfig
		views      map[string]config.ViewConfig
		globalType *config.TypeOverride
		column     string
		wantErr    string
		wantNoErr  string
	}{
		{
			name:    "no entity carries the column",
			schema:  &parser.Schema{Tables: []parser.Table{widgets}},
			wantErr: refusal,
		},
		{
			// A global tenancy.type does not stand in: it types the tenant
			// column, and there is no tenant column to type.
			name:       "a global tenancy.type does not supply the type",
			schema:     &parser.Schema{Tables: []parser.Table{widgets}},
			globalType: &config.TypeOverride{Type: "int64"},
			wantErr:    refusal,
		},
		{
			name:    "every carrier opts out",
			schema:  &parser.Schema{Tables: []parser.Table{products, widgets}, Views: []parser.View{stats}},
			tables:  map[string]config.TableConfig{"products": {Tenancy: &config.TableTenancyConfig{Enabled: new(false)}}},
			views:   map[string]config.ViewConfig{"product_stats": {Tenancy: &config.TableTenancyConfig{Enabled: new(false)}}},
			wantErr: refusal,
		},
		{
			name:    "the error names the configured tenancy.column",
			schema:  &parser.Schema{Tables: []parser.Table{products}},
			column:  "workspce_id",
			wantErr: `no generated table or view carries tenant column "workspce_id"`,
		},
		{
			name:   "a tenanted view alone is enough",
			schema: &parser.Schema{Tables: []parser.Table{widgets}, Views: []parser.View{stats}},
		},
		{
			// The per-entity error already says why the entity is not
			// tenanted; the package-level refusal would only repeat it.
			name:      "a per-entity error is not joined by the refusal",
			schema:    &parser.Schema{Tables: []parser.Table{widgets}},
			tables:    map[string]config.TableConfig{"widgets": {Tenancy: &config.TableTenancyConfig{Enabled: new(true)}}},
			wantErr:   `table public.widgets declares tenancy.enabled=true but tenant column "workspace_id" is not present`,
			wantNoErr: refusal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tenancyTestConfig()
			cfg.Tenancy.Type = tt.globalType
			if tt.column != "" {
				cfg.Tenancy.Column = tt.column
			}
			if tt.tables != nil {
				cfg.Tables = tt.tables
			}
			cfg.Views = tt.views

			_, _, err := gen.BuildTenancyContext(cfg, tt.schema, tenancyTestResolver(cfg))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("BuildTenancyContext() err = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("BuildTenancyContext() err = nil, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("BuildTenancyContext() err = %q, want it to contain %q", err, tt.wantErr)
			}
			if tt.wantNoErr != "" && strings.Contains(err.Error(), tt.wantNoErr) {
				t.Errorf("BuildTenancyContext() err = %q, want it not to contain %q", err, tt.wantNoErr)
			}
		})
	}
}

// Detection covers generated entities only (§29.2.3): a table exclude_tables
// drops (§6.4) or that has no resolved primary key (§9.4b) neither fails a
// tenancy rule nor counts as the tenanted entity the refusal looks for.
func TestBuildTenancyContext_skipsTablesThatDoNotGenerate(t *testing.T) {
	widgets := parser.Table{
		Name:   "widgets",
		Schema: "public",
		Columns: []parser.Column{
			tableCol("id", "integer", false, true),
			tableCol("workspace_id", "bigint", false, false),
		},
	}
	sharedWidgets := parser.Table{
		Name:   "widgets",
		Schema: "public",
		Columns: []parser.Column{
			tableCol("id", "integer", false, true),
			tableCol("name", "text", false, false),
		},
	}
	auditLogs := func(nullable, pk bool) parser.Table {
		return parser.Table{
			Name:   "audit_logs",
			Schema: "public",
			Columns: []parser.Column{
				tableCol("id", "integer", false, pk),
				tableCol("workspace_id", "text", nullable, false),
			},
		}
	}
	const refusal = `no table or view is tenanted: no generated table or view carries tenant column "workspace_id"`

	tests := []struct {
		name         string
		tables       []parser.Table
		exclude      []string
		wantErr      string
		wantTenanted string
	}{
		{
			name:         "an excluded table's divergent tenant type is not checked",
			tables:       []parser.Table{widgets, auditLogs(false, true)},
			exclude:      []string{"audit_logs"},
			wantTenanted: "int64",
		},
		{
			name:         "an excluded table's nullable tenant column is not checked",
			tables:       []parser.Table{widgets, auditLogs(true, true)},
			exclude:      []string{"public.audit_*"},
			wantTenanted: "int64",
		},
		{
			name:         "a table with no primary key is not checked",
			tables:       []parser.Table{widgets, auditLogs(false, false)},
			wantTenanted: "int64",
		},
		{
			name:    "an excluded table does not satisfy the no-tenanted-entity refusal",
			tables:  []parser.Table{sharedWidgets, auditLogs(false, true)},
			exclude: []string{"audit_logs"},
			wantErr: refusal,
		},
		{
			name:    "a table with no primary key does not satisfy the no-tenanted-entity refusal",
			tables:  []parser.Table{sharedWidgets, auditLogs(false, false)},
			wantErr: refusal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tenancyTestConfig()
			cfg.ExcludeTables = tt.exclude

			got, _, err := gen.BuildTenancyContext(cfg, &parser.Schema{Tables: tt.tables}, tenancyTestResolver(cfg))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("BuildTenancyContext() err = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("BuildTenancyContext() err = %v, want nil", err)
			}
			if _, ok := got["public.audit_logs"]; ok {
				t.Errorf("tenancy map has an entry for public.audit_logs, which does not generate")
			}
			if gotType, _ := gen.FirstTenantedType(got); gotType != tt.wantTenanted {
				t.Errorf("FirstTenantedType() = %q, want %q", gotType, tt.wantTenanted)
			}
		})
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
