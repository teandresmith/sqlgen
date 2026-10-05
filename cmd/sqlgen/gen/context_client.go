package gen

import (
	"slices"
	"strings"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// BuildClientContext builds the unified client context from table and view contexts.
// clientName is the exported struct name (default "Client").
// eventsEnabled indicates whether event publishing is enabled globally.
// cache, when non-nil, overlays the cache-specific fields onto the returned context.
// cfg + tenancyMap drive the tenancy-option emission (WithTenantResolver +
// tenantResolver wire-up). tenancyMap is nil when tenancy is globally disabled.
func BuildClientContext(tables []TableContext, views []ViewContext, pkg string, clientName string, eventsEnabled bool, cache *CacheContext, cfg *config.RootConfig, tenancyMap map[string]TenancyContext) ClientContext {
	entities := make([]EntityClientContext, 0, len(tables)+len(views))

	for _, t := range tables {
		entities = append(entities, EntityClientContext{
			StructName:    t.StructName,
			InterfaceName: t.StructName + "Client",
			FieldName:     toCamelCase(t.StructName),
			AccessorName:  entityAccessorName(t.StructName, false),
			IsView:        false,
			ClientWires:   entityClientWires(t),
		})
	}

	for _, v := range views {
		entities = append(entities, EntityClientContext{
			StructName:    v.StructName,
			InterfaceName: v.StructName + "Client",
			FieldName:     toCamelCase(v.StructName),
			AccessorName:  entityAccessorName(v.StructName, true),
			IsView:        true,
		})
	}

	slices.SortFunc(entities, func(a, b EntityClientContext) int {
		return strings.Compare(a.StructName, b.StructName)
	})

	var nestedSurfaceEntities []string
	for _, t := range tables {
		if t.HasNestedSurface {
			nestedSurfaceEntities = append(nestedSurfaceEntities, toCamelCase(t.StructName))
		}
	}
	slices.Sort(nestedSurfaceEntities)

	imports := []string{
		"context",
		"errors",
		"io",
		"github.com/teandresmith/sqlgen/database",
		"github.com/teandresmith/sqlgen/hook",
	}

	// The MySQL server-version probe is the only block in the client template
	// that reaches for these four, and it is emitted under the same predicate
	// (`{{ if eq .Dialect "mysql" }}`). Declaring them here rather than leaving
	// goimports to infer them keeps the emitted file self-contained and keeps
	// resolution off its module-cache scan (modelTemplateImports).
	if cfg != nil && cfg.Input.Dialect == config.DialectMySQL {
		imports = append(imports, "fmt", "strconv", "strings", "sync")
	}

	if eventsEnabled {
		imports = append(imports, "github.com/teandresmith/sqlgen/event")
	}

	ctx := ClientContext{
		Entities:              entities,
		ClientName:            clientName,
		Package:               pkg,
		EventsEnabled:         eventsEnabled,
		Imports:               imports,
		NestedSurfaceEntities: nestedSurfaceEntities,
	}
	if cfg != nil {
		ctx.Dialect = string(cfg.Input.Dialect)
	}

	if cache != nil {
		ctx.CacheEnabled = true
		ctx.CacheConfig = cache.Config
		ctx.CachedTables = cache.CachedTables
		ctx.CachedViews = cache.CachedViews
		ctx.ViewInvalidateMap = cache.ViewInvalidateMap
		imports = append(imports, "github.com/teandresmith/sqlgen/cache")
		slices.Sort(imports)
		imports = slices.Compact(imports)
		ctx.Imports = imports
	}

	// Tenancy wiring — globally enabled projects get WithTenantResolver plus a
	// tenantResolver field, and tenanted-table entity clients get the resolver
	// threaded in at construction time (PRD §29.3.2 / §29.3.3).
	if cfg != nil && cfg.Tenancy != nil && cfg.Tenancy.Enabled {
		applyClientTenancy(&ctx, tables, views, tenancyMap)
	}

	return ctx
}

// entityClientWires lists the cross-client references one entity client needs
// wired up after construction: the target client behind every O2M and M2M edge
// (relationship loading), and every client the nested-mutation executors write
// through beyond those — each M2M edge's junction and each has-one edge's
// target (PRD §9.9.6).
//
// One `seen` set spans all three loops, so a target reached by two edges — and
// a junction or has-one target that is also an O2M / M2M target — contributes
// one wire. The nested half is rendered from NestedWriteClients, the same
// slice the entity client's field list comes from, so a wire can never name a
// field that was not declared.
func entityClientWires(t TableContext) []ClientWire {
	seen := make(map[string]bool)
	var wires []ClientWire

	add := func(structName string) {
		entityField := toCamelCase(structName) + "Client"
		if seen[entityField] {
			return
		}
		seen[entityField] = true
		wires = append(wires, ClientWire{EntityField: entityField, TargetField: toCamelCase(structName)})
	}

	for _, rel := range t.O2MRelationships {
		add(rel.TargetStructName)
	}
	for _, rel := range t.M2MRelationships {
		add(rel.TargetStructName)
	}
	for _, name := range t.NestedWriteClients {
		add(name)
	}
	return wires
}

// entityAccessorName spells the unified client's accessor method for one
// entity: plural for a table (`Client.Products()`), singular for a view
// (`Client.ProductSummary()`).
//
// A view is named for the relation it projects, which is already whatever the
// author called it, so pluralizing would rename it — `Client.ProductSummaries()`
// for a view called `product_summary`. Tables pluralize because the accessor
// names a collection.
//
// One function, because two surfaces read it: the client template (through
// EntityClientContext.AccessorName) and the GraphQL resolver template (through
// APITableContext.ClientAccessor). The resolver calls the method the client
// declares, so a second spelling here is a compile error waiting for the first
// project that has both a view and an API.
func entityAccessorName(structName string, isView bool) string {
	if isView {
		return structName
	}
	return StructNamePlural(structName)
}

// applyClientTenancy fills the tenancy-dependent fields on ctx: GoType,
// Import, the sorted list of tenanted entity field names (tables and views
// alike), and the required imports. Factored out of BuildClientContext to keep
// the latter under the cyclomatic-complexity limit.
func applyClientTenancy(ctx *ClientContext, tables []TableContext, views []ViewContext, tenancyMap map[string]TenancyContext) {
	ctx.TenancyEnabled = true
	goType, importPath := FirstTenantedType(tenancyMap)
	ctx.TenancyGoType = goType
	ctx.TenancyImport = importPath

	// The set is wider than "detected as tenanted": a parent with a tenanted
	// O2O child resolves at GetMany time to append the child-side filter
	// (PRD §29.10); a table whose relationship filter can reach a tenanted
	// target resolves to put that target's predicate inside the EXISTS
	// (§11.1 invariant 1); and a parent with a tenanted nested edge resolves
	// once for the whole nested write (§29.6). None of the three
	// requires a tenant column on the table itself.
	//
	// The union is decided once, by TableContext.NeedsTenantResolver, and read
	// here —
	// restating it would let this list and the field the template emits drift,
	// which is an assignment to a field that does not exist.
	var tenanted []string
	for _, t := range tables {
		if t.NeedsTenantResolver() {
			tenanted = append(tenanted, toCamelCase(t.StructName))
		}
	}
	// Tenanted views need the resolver threaded in for the same reason tables
	// do — their five read methods scope on the tenant column (PRD §29.2.5).
	// A view has no O2O children, so detection is the whole condition.
	for _, v := range views {
		if v.Tenancy != nil && v.Tenancy.Tenanted {
			tenanted = append(tenanted, toCamelCase(v.StructName))
		}
	}
	slices.Sort(tenanted)
	ctx.TenantedEntities = tenanted

	imports := append([]string(nil), ctx.Imports...)
	imports = append(imports, "github.com/teandresmith/sqlgen/tenancy")
	if importPath != "" {
		imports = append(imports, importPath)
	}
	slices.Sort(imports)
	ctx.Imports = slices.Compact(imports)
}
