package gen_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// The tests in this file cover PRD §29.2.4 steps 1-2 reaching the tenant
// column itself, not only the tenancy context: `tenancy.type` resolves **the
// tenant column's** Go type, so the row struct, CreateInput, filter and PK
// struct must spell it the same way TenantResolver[T] does.
//
// Before this, the override stopped at TenancyContext. A package with
// `tenancy.type: {type: uuid.UUID, import: github.com/google/uuid}` and no
// matching column override emitted `WorkspaceID string` on the row struct
// beside `resolvedTenant uuid.UUID`, so the generated `v != resolvedTenant`
// compared two different types and the package did not compile. The same gap
// hid the override's import from the one-UUID-library-per-package rule, which
// reads column claims — a surface no column owns is a surface it cannot see
// (the mirror of the column-override fix, which covers the other direction).

// tenantColumnOf returns the named column's context, failing the test when the
// entity does not carry it.
func tenantColumnOf(t *testing.T, cols []gen.ColumnContext, name string) gen.ColumnContext {
	t.Helper()
	for i := range cols {
		if cols[i].Name == name {
			return cols[i]
		}
	}
	t.Fatalf("no column %q in the built context", name)
	return gen.ColumnContext{}
}

// tenantTypeSchema is the fixture every table case below shares: one tenanted
// table whose `workspace_id` is the tenant column, plus a non-tenant `uuid`
// column that must be left alone by a tenancy override.
func tenantTypeSchema() *parser.Schema {
	return &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "projects",
				Schema: "public",
				Columns: []parser.Column{
					tableCol("id", "uuid", false, true),
					tableCol("workspace_id", "uuid", false, false),
					tableCol("owner_id", "uuid", false, false),
				},
			},
		},
	}
}

// TestBuildEntityContexts_TenancyTypeReachesTenantColumn pins the rule on the
// column contexts the row struct is emitted from, across every way §29.2.4
// can resolve — and on the one shape that must *not* take the override, a
// table tenancy is switched off for.
func TestBuildEntityContexts_TenancyTypeReachesTenantColumn(t *testing.T) {
	workspaceID := config.TypeOverride{Type: "WorkspaceID", Import: "example.com/ids"}
	orgID := config.TypeOverride{Type: "OrgID", Import: "example.com/org"}

	tests := []struct {
		name string
		// globalType is the `tenancy.type` block, nil for none.
		globalType *config.TypeOverride
		tableCfg   config.TableConfig
		wantGoType string
		wantImport string
		// wantZeroValue is checked only when set; every other case leaves the
		// derived spelling alone.
		wantZeroValue string
		// globalOff turns the master switch off, leaving the rest of the
		// tenancy block in place.
		globalOff bool
		// wantTenanted reports whether the table should still be tenanted, so
		// the case also pins that an override never changes detection.
		wantTenanted bool
		// wantNoTenancyCtx expects no TableTenancyContext at all, which is what
		// a globally-disabled block produces.
		wantNoTenancyCtx bool
	}{
		{
			name:         "global tenancy.type types the tenant column",
			globalType:   &workspaceID,
			wantGoType:   "WorkspaceID",
			wantImport:   "example.com/ids",
			wantTenanted: true,
		},
		{
			name:       "per-table tenancy.type beats the global block",
			globalType: &workspaceID,
			tableCfg: config.TableConfig{
				Tenancy: &config.TableTenancyConfig{Type: &orgID},
			},
			wantGoType:   "OrgID",
			wantImport:   "example.com/org",
			wantTenanted: true,
		},
		{
			name:       "tenancy.type beats column_map on the same column",
			globalType: &workspaceID,
			tableCfg: config.TableConfig{
				ColumnMap: map[string]config.ColumnOverride{
					"workspace_id": {Type: "uuid.UUID", Import: "github.com/google/uuid"},
				},
			},
			wantGoType:   "WorkspaceID",
			wantImport:   "example.com/ids",
			wantTenanted: true,
		},
		{
			// §29.2.4 step 3: with no tenancy.type the column keeps its own
			// resolution, which is what already kept the two spellings equal.
			name:         "no tenancy.type leaves the column's own resolution",
			wantGoType:   "uuid.UUID",
			wantImport:   "github.com/google/uuid",
			wantTenanted: true,
		},
		{
			// FromLiteral can only derive `T{}` for a type it does not know,
			// which is the wrong spelling whenever the zero is a package
			// variable rather than a composite literal — the shape gofrs's
			// `uuid.Nil` has, and the reason the field exists. The override
			// carries it, so it has to reach the column. (Spelled here with a
			// non-UUID wrapper so the case tests the zero_value plumbing and
			// not the one-library rule, which owns its own tests below.)
			name:          "tenancy.type zero_value reaches the column",
			globalType:    &config.TypeOverride{Type: "WorkspaceID", Import: "example.com/ids", ZeroValue: "ids.NilWorkspace"},
			wantGoType:    "WorkspaceID",
			wantImport:    "example.com/ids",
			wantZeroValue: "ids.NilWorkspace",
			wantTenanted:  true,
		},
		{
			// `tenancy.enabled` is the master switch: BuildTenancyContext
			// returns a nil map the moment it is false, so a per-table
			// `enabled: true` underneath it still yields no tenancy codegen
			// (§29.2.1). The override must not retype the column either —
			// reading the per-entity accessor alone would, because it answers
			// the narrower "which setting wins for this entity".
			name:       "globally disabled tenancy declines the override",
			globalOff:  true,
			globalType: &workspaceID,
			tableCfg: config.TableConfig{
				Tenancy: &config.TableTenancyConfig{Enabled: new(true)},
			},
			wantGoType: "uuid.UUID",
			wantImport: "github.com/google/uuid",
			// No tenancy map at all, so no TableTenancyContext is attached.
			wantNoTenancyCtx: true,
		},
		{
			// A table tenancy is switched off for has no tenant column, so the
			// override must not retype anything on it (§29.2.3 rule 4).
			name:       "per-table opt-out declines the override",
			globalType: &workspaceID,
			tableCfg: config.TableConfig{
				Tenancy: &config.TableTenancyConfig{Enabled: new(false)},
			},
			wantGoType:   "uuid.UUID",
			wantImport:   "github.com/google/uuid",
			wantTenanted: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tenancyTestConfig()
			cfg.Overrides.Types["uuid"] = uuidGoogle
			cfg.Tenancy.Type = tt.globalType
			cfg.Tenancy.Enabled = !tt.globalOff
			cfg.Tables["public.projects"] = tt.tableCfg
			schema := tenantTypeSchema()

			tables, _, err := gen.BuildEntityContextsFromSchema(schema, cfg)
			if err != nil {
				t.Fatalf("BuildEntityContextsFromSchema() error = %v, want nil", err)
			}
			if len(tables) != 1 {
				t.Fatalf("BuildEntityContextsFromSchema() built %d tables, want 1", len(tables))
			}

			tenant := tenantColumnOf(t, tables[0].Columns, "workspace_id")
			if tenant.GoType != tt.wantGoType {
				t.Errorf("column workspace_id GoType = %q, want %q", tenant.GoType, tt.wantGoType)
			}
			if tenant.Import != tt.wantImport {
				t.Errorf("column workspace_id Import = %q, want %q", tenant.Import, tt.wantImport)
			}
			if tt.wantZeroValue != "" && tenant.ZeroValue != tt.wantZeroValue {
				t.Errorf("column workspace_id ZeroValue = %q, want %q", tenant.ZeroValue, tt.wantZeroValue)
			}

			// A non-tenant column of the same SQL type must be untouched —
			// the override is scoped to the tenant column, not to `uuid`.
			owner := tenantColumnOf(t, tables[0].Columns, "owner_id")
			if owner.GoType != "uuid.UUID" || owner.Import != "github.com/google/uuid" {
				t.Errorf("column owner_id = (%q, %q), want the unchanged (%q, %q)",
					owner.GoType, owner.Import, "uuid.UUID", "github.com/google/uuid")
			}

			// The point of the fix: one column, one spelling. When the table
			// is tenanted, the row struct and TenantResolver[T] must agree.
			tenancyCtx := tables[0].Tenancy
			if tt.wantNoTenancyCtx {
				if tenancyCtx != nil && tenancyCtx.Tenanted {
					t.Errorf("TableContext.Tenancy = %+v, want no tenanted context under a disabled global block", tenancyCtx)
				}
				return
			}
			if tenancyCtx == nil {
				t.Fatal("TableContext.Tenancy = nil, want it attached")
			}
			if tenancyCtx.Tenanted != tt.wantTenanted {
				t.Fatalf("TableContext.Tenancy.Tenanted = %v, want %v", tenancyCtx.Tenanted, tt.wantTenanted)
			}
			if !tt.wantTenanted {
				return
			}
			if tenancyCtx.GoType != tenant.GoType {
				t.Errorf("tenancy context GoType = %q, row struct spells %q; one column must have one spelling",
					tenancyCtx.GoType, tenant.GoType)
			}
			if tenancyCtx.Import != tenant.Import {
				t.Errorf("tenancy context Import = %q, row struct spells %q; one column must have one import",
					tenancyCtx.Import, tenant.Import)
			}
		})
	}
}

// TestBuildEntityContexts_TenancyTypeReachesViewTenantColumn covers the view
// path, which had the identical gap: resolveViewTenantColumnType returned the
// override while buildViewColumns resolved the row struct with no tenancy
// awareness.
//
// The nullable case is the one place the two spellings are *meant* to differ.
// §29.2.5 permits a nullable tenant column on a view — a LEFT JOIN or an
// aggregate legitimately widens a NOT NULL base column — so the row struct
// keeps the nullable spelling while the tenancy context carries the plain `T`
// a TenantResolver[T] yields. The override must supply the type without
// flattening that distinction.
func TestBuildEntityContexts_TenancyTypeReachesViewTenantColumn(t *testing.T) {
	workspaceID := config.TypeOverride{Type: "WorkspaceID", Import: "example.com/ids"}

	tests := []struct {
		name           string
		nullable       bool
		viewCfg        *config.ViewConfig
		wantGoType     string
		wantImport     string
		wantContextTyp string
	}{
		{
			name:           "global tenancy.type types the view's tenant column",
			wantGoType:     "WorkspaceID",
			wantImport:     "example.com/ids",
			wantContextTyp: "WorkspaceID",
		},
		{
			name:       "per-view tenancy.type beats the global block",
			viewCfg:    &config.ViewConfig{Tenancy: &config.TableTenancyConfig{Type: &config.TypeOverride{Type: "OrgID", Import: "example.com/org"}}},
			wantGoType: "OrgID",
			wantImport: "example.com/org",

			wantContextTyp: "OrgID",
		},
		{
			// Row struct keeps `*WorkspaceID`; the resolver still yields a
			// plain WorkspaceID. Both are correct, and deliberately different.
			name:           "nullable view tenant column keeps the nullable row spelling",
			nullable:       true,
			wantGoType:     "*WorkspaceID",
			wantImport:     "example.com/ids",
			wantContextTyp: "WorkspaceID",
		},
		{
			name:           "per-view opt-out declines the override",
			viewCfg:        &config.ViewConfig{Tenancy: &config.TableTenancyConfig{Enabled: new(false)}},
			wantGoType:     "uuid.UUID",
			wantImport:     "github.com/google/uuid",
			wantContextTyp: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tenancyTestConfig()
			cfg.Overrides.Types["uuid"] = uuidGoogle
			cfg.Tenancy.Type = &workspaceID
			cfg.Views = make(map[string]config.ViewConfig)
			if tt.viewCfg != nil {
				cfg.Views["order_stats"] = *tt.viewCfg
			}
			schema := viewSchema("order_stats",
				viewCol("id", "uuid", false),
				viewCol("workspace_id", "uuid", tt.nullable),
			)

			_, views, err := gen.BuildEntityContextsFromSchema(schema, cfg)
			if err != nil {
				t.Fatalf("BuildEntityContextsFromSchema() error = %v, want nil", err)
			}
			if len(views) != 1 {
				t.Fatalf("BuildEntityContextsFromSchema() built %d views, want 1", len(views))
			}

			tenant := tenantColumnOf(t, views[0].Columns, "workspace_id")
			if tenant.GoType != tt.wantGoType {
				t.Errorf("view column workspace_id GoType = %q, want %q", tenant.GoType, tt.wantGoType)
			}
			if tenant.Import != tt.wantImport {
				t.Errorf("view column workspace_id Import = %q, want %q", tenant.Import, tt.wantImport)
			}

			tenancyMap, _, err := gen.BuildTenancyContext(cfg, schema, tenancyTestResolver(cfg))
			if err != nil {
				t.Fatalf("BuildTenancyContext() error = %v, want nil", err)
			}
			got := tenancyMap["public.order_stats"]
			if got.GoType != tt.wantContextTyp {
				t.Errorf("tenancy context GoType = %q, want %q", got.GoType, tt.wantContextTyp)
			}
		})
	}
}

// TestGenerate_TenancyTypeSelectsUUIDIntegration is the end-to-end failing-
// first case. `tenancy.type` names google and no `overrides.types.uuid` is
// configured, so before the fix nothing in the package claimed a UUID library:
// selectUUIDIntegration fell back to the standard library and
// attachUUIDGeneration declared `"uuid"` beside the `"github.com/google/uuid"`
// tenancy had already put there — `uuid redeclared in this block`.
//
// The tenant column now carries the claim, so the package selects google, and
// the row struct spells the tenant the way the resolver does.
func TestGenerate_TenancyTypeSelectsUUIDIntegration(t *testing.T) {
	outDir := t.TempDir()
	cfg := loadLayoutConfig(t, outDir, "single_file")
	cfg.Events = &config.EventConfig{Enabled: true}
	cfg.Tenancy = &config.TenancyConfig{
		Enabled:  true,
		Column:   "workspace_id",
		Required: new(true),
		Type:     &uuidGoogle,
	}
	// The PK is retyped to a Go `string`. That is what leaves the package with
	// no uuid-qualified column outside the tenant one, which is the state this
	// test is about: since `uuid` resolves to `uuid.UUID` by default
	// (PRD §7.2), a bare `uuid` PK would claim the standard library and the
	// package would hold two libraries rather than none. It is also the case
	// PRD §7.4's step-2 selection rule names outright — a `uuid` primary key
	// overridden to a Go `string` still has to generate with the library the
	// config declares.
	cfg.Tables = map[string]config.TableConfig{
		"orders": {
			ColumnMap: map[string]config.ColumnOverride{
				"id": {Type: "string"},
			},
		},
	}
	schema := &parser.Schema{
		Tables: []parser.Table{{
			Name: "orders",
			Columns: []parser.Column{
				// No DEFAULT, so PKStrategy resolves to "app" and the create
				// path emits a generating call that declares its own import.
				// PKStrategy keys on the SQL type, so the retype above does
				// not move it off "app".
				tableCol("id", "uuid", false, true),
				tableCol("workspace_id", "uuid", false, false),
				tableCol("label", "text", false, false),
			},
		}},
	}

	if _, err := gen.Generate(schema, cfg, "test"); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	assertOnlyUUIDLibrary(t, outDir, "github.com/google/uuid")

	body := readGenerated(t, filepath.Join(outDir, "models_gen.go"))
	for _, want := range []string{
		// The tenant column on the row struct, spelled as the resolver spells it.
		"WorkspaceID uuid.UUID",
		// google's v4 string form — the selected integration, not the fallback.
		"pkValue = uuid.NewString()",
		"tenancy.TenantResolver[uuid.UUID]",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("models_gen.go missing %q", want)
		}
	}
	if got := "uuid.NewV4().String()"; strings.Contains(body, got) {
		t.Errorf("models_gen.go contains %q — the standard library fallback, not the selected integration", got)
	}
}

// TestGenerate_TenancyTypeDisagreeingWithColumnRejected is the other half:
// when `tenancy.type` and a column genuinely bind two different libraries, the
// package cannot compile and the one-library rule must refuse it — naming both
// entities, so the message points at a config the consumer can actually change.
//
// This is the `tenancy` example's UUID migration in miniature: move
// `overrides.types` to the standard library and leave `tenancy.type` on google.
func TestGenerate_TenancyTypeDisagreeingWithColumnRejected(t *testing.T) {
	outDir := t.TempDir()
	cfg := loadLayoutConfig(t, outDir, "single_file")
	cfg.Overrides.Types = map[string]config.TypeOverride{"uuid": uuidStdlib}
	cfg.Tenancy = &config.TenancyConfig{
		Enabled:  true,
		Column:   "workspace_id",
		Required: new(true),
		Type:     &uuidGoogle,
	}
	// orders' own PK is a bigint, so the only stdlib claim in the package
	// comes from workspaces.id — the two libraries are bound by two different
	// entities, which is what the message has to be able to say.
	schema := &parser.Schema{
		Tables: []parser.Table{
			{Name: "orders", Columns: []parser.Column{
				{Name: "id", Type: "bigint", PrimaryKey: true, AutoIncrement: true},
				tableCol("workspace_id", "uuid", false, false),
			}},
			{Name: "workspaces", Columns: []parser.Column{
				tableCol("id", "uuid", false, true),
			}},
		},
	}

	_, err := gen.Generate(schema, cfg, "test")
	if err == nil {
		t.Fatal("Generate() error = nil, want two UUID libraries to be rejected")
	}
	for _, want := range []string{
		`"uuid"`,
		`"github.com/google/uuid"`,
		`table orders`,
		`table workspaces`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Generate() error = %v, want it to mention %q", err, want)
		}
	}
	if got := dirEntries(t, outDir); len(got) != 0 {
		t.Errorf("Generate() wrote %v before rejecting the config; want nothing on disk", got)
	}
}

// TestValidateAndGenerate_AgreeOnTenancyUUIDSurface pins the property that
// made the claims-only shape of this fix wrong: a rule that reads a surface
// only `generate` assembles is a rule `validate` reports clean on. The tenant
// column is a column, so all three entry points see it through the same
// claim — there is no second code path to keep in step.
func TestValidateAndGenerate_AgreeOnTenancyUUIDSurface(t *testing.T) {
	outDir := t.TempDir()
	cfg := loadLayoutConfig(t, outDir, "single_file")
	cfg.Overrides.Types = map[string]config.TypeOverride{"uuid": uuidStdlib}
	cfg.Tenancy = &config.TenancyConfig{
		Enabled:  true,
		Column:   "workspace_id",
		Required: new(true),
		Type:     &uuidGoogle,
	}
	// orders' own PK is a bigint, so the only stdlib claim in the package
	// comes from workspaces.id — the two libraries are bound by two different
	// entities, which is what the message has to be able to say.
	schema := &parser.Schema{
		Tables: []parser.Table{
			{Name: "orders", Columns: []parser.Column{
				{Name: "id", Type: "bigint", PrimaryKey: true, AutoIncrement: true},
				tableCol("workspace_id", "uuid", false, false),
			}},
			{Name: "workspaces", Columns: []parser.Column{
				tableCol("id", "uuid", false, true),
			}},
		},
	}

	_, genErr := gen.Generate(schema, cfg, "test")
	if genErr == nil {
		t.Fatal("Generate() error = nil, want two UUID libraries to be rejected")
	}

	_, validateErr := gen.ValidateGeneration(schema, cfg)
	if validateErr == nil {
		t.Fatal("ValidateGeneration() error = nil, want the same rejection generate gave")
	}
	if !strings.Contains(validateErr.Error(), genErr.Error()) {
		t.Errorf("ValidateGeneration() error = %q, want it to carry generate's message %q", validateErr, genErr)
	}

	// `sqlgen graphql gen` is the third entry point and reaches the context
	// builders directly.
	if _, _, err := gen.BuildEntityContextsFromSchema(schema, cfg); err == nil {
		t.Error("BuildEntityContextsFromSchema() error = nil, want the same rejection")
	}
}

// TestBuildEntityContexts_TenancyTypeHonorsFullOverrideShape pins the promise
// §29.2.4 makes by calling `tenancy.type` "a `TypeOverride` (same shape as
// section 4.7)": every key §4.7 honors has to reach the tenant column, not just
// `type` and `import`.
//
// The keys below all validated against the config schema and then did nothing,
// which is the defect this test guards. They are reachable now because the
// override resolves through gotype.FromOverride — the same function every
// `overrides.types` entry goes through — rather than through a hand-rolled
// spelling of the literal.
func TestBuildEntityContexts_TenancyTypeHonorsFullOverrideShape(t *testing.T) {
	// `nullable` on a view's tenant column — the only surface it can apply to,
	// since a table's tenant column may not be nullable (§29.2.4) and a view's
	// may (§29.2.5). Without the variant the row struct would spell `*T`.
	t.Run("nullable variant reaches the view tenant column", func(t *testing.T) {
		cfg := tenancyTestConfig()
		cfg.Overrides.Types["uuid"] = uuidGoogle
		cfg.Tenancy.Type = &config.TypeOverride{
			Type:     "uuid.UUID",
			Import:   "github.com/google/uuid",
			Nullable: config.NullableVariant{Type: "uuid.NullUUID", UnderlyingField: "UUID"},
		}
		cfg.Views = make(map[string]config.ViewConfig)
		schema := viewSchema("order_stats",
			viewCol("id", "uuid", false),
			viewCol("workspace_id", "uuid", true),
		)

		_, views, err := gen.BuildEntityContextsFromSchema(schema, cfg)
		if err != nil {
			t.Fatalf("BuildEntityContextsFromSchema() error = %v, want nil", err)
		}
		tenant := tenantColumnOf(t, views[0].Columns, "workspace_id")
		if tenant.GoType != "uuid.NullUUID" {
			t.Errorf("view column workspace_id GoType = %q, want %q — the declared nullable variant, not the derived pointer", tenant.GoType, "uuid.NullUUID")
		}
		if tenant.Import != "github.com/google/uuid" {
			t.Errorf("view column workspace_id Import = %q, want %q", tenant.Import, "github.com/google/uuid")
		}

		// The tenancy context is unaffected: TenantResolver[T] yields the plain
		// value type, never the wrapper (§29.2.5).
		tenancyMap, _, err := gen.BuildTenancyContext(cfg, schema, tenancyTestResolver(cfg))
		if err != nil {
			t.Fatalf("BuildTenancyContext() error = %v, want nil", err)
		}
		if got := tenancyMap["public.order_stats"].GoType; got != "uuid.UUID" {
			t.Errorf("tenancy context GoType = %q, want the plain value type %q", got, "uuid.UUID")
		}
	})
}
