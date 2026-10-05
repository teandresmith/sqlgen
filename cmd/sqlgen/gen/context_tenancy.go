package gen

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/parser"
)

// TenancyContext holds the resolved tenancy metadata for one parsed table.
// Every table in a tenancy-enabled schema gets a TenancyContext; Tenanted
// differentiates the auto-filtered tables from shared / opted-out tables so
// downstream codegen can emit tenancy blocks only where relevant
// (PRD §29.2.3).
//
// Column / GoType / Import / Required are only meaningful when Tenanted is
// true. On shared tables these fields are zero.
type TenancyContext struct {
	Tenanted bool
	Column   string
	Required bool
	GoType   string
	Import   string
}

// tenantedEntry is the per-entity scratch record used while validating the
// uniform-type invariant across the whole generate run (§29.2.4). Views
// participate alongside tables — the package exposes exactly one concretely
// typed TenantResolver[T], so a view whose tenant column resolves to a
// different Go type has no resolver to draw from (§29.2.5).
type tenantedEntry struct {
	schema string
	name   string
	goType string
	imp    string
	isView bool
}

// label renders the entry for the aggregated uniform-type error. Views are
// spelled out so the offender list says which config key opts them out.
func (e tenantedEntry) label() string {
	if e.isView {
		return "view " + qualifiedTableName(e.schema, e.name)
	}
	return qualifiedTableName(e.schema, e.name)
}

// BuildTenancyContext runs the detection + type-resolution + validation pass
// described in PRD §29.2.3 and §29.2.4. The return map is keyed by
// schema-qualified name (or bare name when the entity has no schema) and
// contains one entry per parsed table **and** one per parsed view — detection
// covers every generated entity, because scoping the tables while leaving a
// tenant-shaped view reading across tenants is the failure mode tenancy
// exists to prevent (§29.2.3, §29.2.5).
//
// Returns a nil map when tenancy is not globally enabled; callers should
// interpret a nil map as "no entity is tenanted". The returned warnings are
// the non-fatal notices detection raised (today: a nullable tenant column on
// a detected view — §29.2.5). All detected validation errors are
// aggregated via errors.Join so every offender surfaces in one pass.
func BuildTenancyContext(cfg *config.RootConfig, schema *parser.Schema, resolver *gotype.Resolver) (map[string]TenancyContext, []string, error) {
	if cfg == nil || cfg.Tenancy == nil || !cfg.Tenancy.Enabled {
		return nil, nil, nil
	}

	result := make(map[string]TenancyContext, len(schema.Tables)+len(schema.Views))
	var errs []error
	var warnings []string
	var tenanted []tenantedEntry

	for i := range schema.Tables {
		table := &schema.Tables[i]
		entry, uniformIn, tableErrs := resolveTableTenancy(cfg, table, resolver)
		result[qualifiedTableName(table.Schema, table.Name)] = entry
		errs = append(errs, tableErrs...)
		if uniformIn != nil {
			tenanted = append(tenanted, *uniformIn)
		}
	}

	for i := range schema.Views {
		view := &schema.Views[i]
		entry, uniformIn, viewWarnings, viewErrs := resolveViewTenancy(cfg, view, resolver)
		result[qualifiedTableName(view.Schema, view.Name)] = entry
		errs = append(errs, viewErrs...)
		warnings = append(warnings, viewWarnings...)
		if uniformIn != nil {
			tenanted = append(tenanted, *uniformIn)
		}
	}

	// Uniform-type validation across every tenanted entity (§29.2.4). One
	// aggregated error, never N separate errors — the list of offenders is
	// the remediation the user needs.
	if err := validateUniformTenantType(tenanted); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return nil, warnings, errors.Join(errs...)
	}
	return result, warnings, nil
}

// resolveTableTenancy computes the TenancyContext for one table and returns
// the per-table validation errors discovered along the way. uniformIn is
// non-nil only when the table is tenanted and participates in the
// cross-table uniform-type check.
func resolveTableTenancy(cfg *config.RootConfig, table *parser.Table, resolver *gotype.Resolver) (TenancyContext, *tenantedEntry, []error) {
	tc := findTableConfig(cfg, table.Name, table.Schema)
	effectiveColumn := config.ResolveTableTenancyColumn(tc, cfg.Tenancy)

	forcedOff := tc.Tenancy != nil && tc.Tenancy.Enabled != nil && !*tc.Tenancy.Enabled
	forcedOn := tc.Tenancy != nil && tc.Tenancy.Enabled != nil && *tc.Tenancy.Enabled

	// Explicit per-table opt-out wins even when the column exists
	// (§29.2.3 rule 4).
	if forcedOff || effectiveColumn == "" {
		return TenancyContext{Tenanted: false}, nil, nil
	}

	col, found := findTenantColumn(table.Columns, effectiveColumn)

	if !found {
		if forcedOn {
			// Deferred from config load: per-table enabled:true + missing
			// column is a hard codegen error.
			return TenancyContext{Tenanted: false}, nil, []error{fmt.Errorf(
				"tenancy: table %s declares tenancy.enabled=true but tenant column %q is not present in the parsed schema; add the column or remove the per-table override",
				qualifiedTableName(table.Schema, table.Name), effectiveColumn,
			)}
		}
		// Column absent with no per-table override → shared table
		// (§29.2.3 rule 3). Detection-based inclusion mirrors soft
		// delete: only opt-outs and oddities need config entries.
		return TenancyContext{Tenanted: false}, nil, nil
	}

	// Nullable check (§29.2.4): NULL tenant contradicts the
	// "one tenant per row" invariant.
	if col.Nullable {
		return TenancyContext{Tenanted: false}, nil, []error{fmt.Errorf(
			"tenancy: table %s tenant column %q must not be nullable; tenant column must not be nullable — add NOT NULL to the column definition",
			qualifiedTableName(table.Schema, table.Name), effectiveColumn,
		)}
	}

	goType, imp, typeErr := resolveTenantColumnType(tc, cfg.Tenancy, col, resolver)
	if typeErr != nil {
		return TenancyContext{Tenanted: false}, nil, []error{fmt.Errorf(
			"tenancy: table %s tenant column %q: %w",
			qualifiedTableName(table.Schema, table.Name), effectiveColumn, typeErr,
		)}
	}

	// Comparable-type validation (§29.2.4 "comparable" rule).
	if reason, ok := nonComparableReason(cfg, goType); ok {
		return TenancyContext{Tenanted: false}, nil, []error{fmt.Errorf(
			"tenancy: table %s tenant type %q is not comparable (%s); tenant types must be `comparable` — use a scalar, string, uuid, or a named wrapper over one of those",
			qualifiedTableName(table.Schema, table.Name), goType, reason,
		)}
	}

	ctx := TenancyContext{
		Tenanted: true,
		Column:   effectiveColumn,
		Required: config.ResolveTableTenancyRequired(tc, cfg.Tenancy),
		GoType:   goType,
		Import:   imp,
	}
	return ctx, &tenantedEntry{
		schema: table.Schema,
		name:   table.Name,
		goType: goType,
		imp:    imp,
	}, nil
}

// resolveViewTenancy computes the TenancyContext for one view and returns the
// warnings and validation errors discovered along the way. uniformIn is
// non-nil only when the view is tenanted and participates in the cross-entity
// uniform-type check (§29.2.4).
//
// It mirrors resolveTableTenancy minus every write-path concern (PRD §29.2.5):
// a view is read-only, so there is no tenant-mismatch check, no SET-clause
// exclusion, and no InPrimaryKey handling — a view has no DDL primary key, and
// §29.7's composite-PK constructor-omission rule is a create-path rule. The
// one behavioral divergence is nullability: a nullable tenant column is a
// legitimate artifact of a LEFT JOIN or an aggregate widening a NOT NULL base
// column, so it warns and still scopes rather than hard-erroring as it
// does on a table.
func resolveViewTenancy(cfg *config.RootConfig, view *parser.View, resolver *gotype.Resolver) (TenancyContext, *tenantedEntry, []string, []error) {
	vc, _ := resolveViewConfig(cfg.Views, view.Schema, view.Name)
	effectiveColumn := config.ResolveViewTenancyColumn(vc, cfg.Tenancy)
	qualified := qualifiedTableName(view.Schema, view.Name)

	forcedOff := vc.Tenancy != nil && vc.Tenancy.Enabled != nil && !*vc.Tenancy.Enabled
	forcedOn := vc.Tenancy != nil && vc.Tenancy.Enabled != nil && *vc.Tenancy.Enabled

	// Explicit per-view opt-out wins even when the column exists
	// (§29.2.3 rule 4).
	if forcedOff || effectiveColumn == "" {
		return TenancyContext{Tenanted: false}, nil, nil, nil
	}

	col, found := findTenantColumn(view.Columns, effectiveColumn)
	if !found {
		if forcedOn {
			// §29.2.3 rule 5, view parity.
			return TenancyContext{Tenanted: false}, nil, nil, []error{fmt.Errorf(
				"tenancy: view %s declares tenancy.enabled=true but tenant column %q is not present in the parsed schema; add the column to the view's projection or remove the per-view override",
				qualified, effectiveColumn,
			)}
		}
		// Column absent with no per-view override → shared view
		// (§29.2.3 rule 3).
		return TenancyContext{Tenanted: false}, nil, nil, nil
	}

	var warnings []string
	if col.Nullable {
		if forcedOn {
			// The user asserted the view is tenant-scoped; a nullable tenant
			// column contradicts the assertion, and silently hiding NULL rows
			// is not an acceptable resolution of it (§29.2.5).
			return TenancyContext{Tenanted: false}, nil, nil, []error{fmt.Errorf(
				"tenancy: view %s declares tenancy.enabled=true but tenant column %q is nullable; a nullable tenant column contradicts the assertion — make the column NOT NULL in the view's projection (COALESCE, an INNER JOIN) or drop the per-view override and let detection scope it with a warning",
				qualified, effectiveColumn,
			)}
		}
		warnings = append(warnings, fmt.Sprintf(
			"tenancy: view %s tenant column %q is nullable; the view is still scoped and `%s = $1` excludes NULL rows, which is fail-closed. A LEFT JOIN or aggregate legitimately widens a NOT NULL base column, so this is a notice, not an error. Set views.%s.tenancy.enabled: false to opt the view out.",
			qualified, effectiveColumn, effectiveColumn, qualified,
		))
	}

	goType, imp, typeErr := resolveViewTenantColumnType(vc, cfg.Tenancy, col, resolver)
	if typeErr != nil {
		return TenancyContext{Tenanted: false}, nil, warnings, []error{fmt.Errorf(
			"tenancy: view %s tenant column %q: %w", qualified, effectiveColumn, typeErr,
		)}
	}

	// Comparable-type validation (§29.2.4 "comparable" rule).
	if reason, ok := nonComparableReason(cfg, goType); ok {
		return TenancyContext{Tenanted: false}, nil, warnings, []error{fmt.Errorf(
			"tenancy: view %s tenant type %q is not comparable (%s); tenant types must be `comparable` — use a scalar, string, uuid, or a named wrapper over one of those",
			qualified, goType, reason,
		)}
	}

	ctx := TenancyContext{
		Tenanted: true,
		Column:   effectiveColumn,
		Required: config.ResolveViewTenancyRequired(vc, cfg.Tenancy),
		GoType:   goType,
		Import:   imp,
	}
	return ctx, &tenantedEntry{
		schema: view.Schema,
		name:   view.Name,
		goType: goType,
		imp:    imp,
		isView: true,
	}, warnings, nil
}

// resolveViewTenantColumnType walks the §29.2.4 type-resolution order for a
// view's tenant column: per-view tenancy.type, then global tenancy.type, then
// the column's own resolution. Views carry no column_map / type_map block, so
// the last rung is viewColumnGoType — the same function that spells the column
// on the row struct, which is what keeps the two spellings from diverging
// (as a table's `column_map.<col>.type` does).
//
// The column is resolved as non-nullable regardless of its own nullability:
// the tenant value bound into `WHERE <tenant> = $1` and carried by
// TenantResolver[T] is a plain T, never a *T or a sql.Null wrapper. The row
// struct keeps the nullable spelling — a view's tenant column may legitimately
// project NULL (§29.2.5) — and those two facts are not in conflict.
func resolveViewTenantColumnType(vc config.ViewConfig, global *config.TenancyConfig, col *parser.Column, resolver *gotype.Resolver) (string, string, error) {
	override := config.ResolveViewTenancyType(vc, global)

	if override != nil {
		if override.Type == "" {
			return "", "", errors.New("tenancy.type override has empty type field")
		}
		if err := checkOverrideAgainstSQLType(override, col); err != nil {
			return "", "", err
		}
		return override.Type, override.Import, nil
	}

	gt := viewColumnGoType(resolver, col, false)
	return gt.Name, gt.Import, nil
}

// findTenantColumn returns the column matching name within a parsed entity's
// column list. Case-insensitive — SQL identifiers on all three supported
// dialects are case-insensitive when unquoted, and tenant column names are
// ASCII in practice. Takes the column slice rather than the table so the
// table and view detection paths share one lookup.
func findTenantColumn(cols []parser.Column, name string) (*parser.Column, bool) {
	for i := range cols {
		if strings.EqualFold(cols[i].Name, name) {
			return &cols[i], true
		}
	}
	return nil, false
}

// resolveTenantColumnType walks the §29.2.4 type-resolution order. Per-table
// tenancy.type wins, then global tenancy.type, then the column's own
// `column_map.<col>.type` override, then the Resolver's chain (steps 3 and
// 4: parsed schema → overrides → integrations → dialect fallback). The
// column_map rung sits inside step 3 alongside `type_map`, which the
// resolver has always consulted there.
//
// When a YAML override is used, a sanity check compares the override
// against the column's parsed SQL type to catch obvious mismatches
// (e.g. declaring WorkspaceID wrapping uuid.UUID against a bigint column).
// The parser's natural resolution is already trusted, so the check is
// skipped when no override is configured.
func resolveTenantColumnType(tc config.TableConfig, global *config.TenancyConfig, col *parser.Column, resolver *gotype.Resolver) (string, string, error) {
	override := config.ResolveTableTenancyType(tc, global)

	if override != nil {
		if override.Type == "" {
			// Config load validates this pre-parse, but constructed-in-test
			// configs bypass LoadConfig — defend in depth.
			return "", "", errors.New("tenancy.type override has empty type field")
		}
		if err := checkOverrideAgainstSQLType(override, col); err != nil {
			return "", "", err
		}
		return override.Type, override.Import, nil
	}

	// Steps 3 + 4: the resolver handles the schema / integration / fallback
	// cascade, with `column_map.<col>.type` ahead of it exactly as on the
	// entity path — otherwise a tenant column's override would reach the
	// row struct but not the tenancy context, and the two spellings of the
	// same column would disagree. Tenant columns are NOT NULL by
	// contract (validated above), so nullable=false — a *string or
	// sql.NullString tenant is never correct.
	//
	// nil tenancy: this is step 3, reached only because steps 1-2 found no
	// override above. Handing the global block back to resolveColumnGoType
	// would ask it to re-apply the override this branch has already declined,
	// which is circular — and, being declined, would change nothing.
	tableOverrides := tableOverridesMap(tc)
	gt := resolveColumnGoType(resolver, col, tc, tableOverrides, nil)
	return gt.Name, gt.Import, nil
}

func tableOverridesMap(tc config.TableConfig) map[string]config.TypeOverride {
	if tc.Overrides == nil {
		return nil
	}
	return tc.Overrides.Types
}

// --- tenancy.type on the column itself (§29.2.4 steps 1-2) ---

// tenantTypeOverrideGoType spells one resolved `tenancy.type` block as a
// GoType at the given nullability, or reports that no override applies.
//
// It defers wholesale to gotype.FromOverride — the same function every
// `overrides.types` entry resolves through — because §29.2.4 specifies
// `tenancy.type` as "a `TypeOverride` (same shape as section 4.7)", and the
// cheapest way to keep a promise of sameness is to run the same code. Spelling
// the type here from the literal instead would re-derive a subset by hand and
// silently drop whatever §4.7 grew that the subset did not: `zero_value` (a
// derived `T{}` is wrong for a zero that is a package variable, which is how
// gofrs spells `uuid.Nil`) and the `nullable` variant. A config key that
// validates and then does nothing is a silent bug, and the tenancy
// block accepts both of them today.
//
// `nullable` is only reachable on some paths, which is a property of the
// surfaces rather than of this function: it cannot apply to a table's tenant
// column, which is NOT NULL by contract (§29.2.4). A view's may legitimately be
// nullable (§29.2.5), and that is the one place the variant is consulted.
func tenantTypeOverrideGoType(override *config.TypeOverride, nullable bool) (gotype.GoType, bool) {
	if override == nil || override.Type == "" {
		return gotype.GoType{}, false
	}
	return gotype.FromOverride(*override, nullable), true
}

// tableTenantTypeOverride returns the Go type a `tenancy.type` override
// imposes on col when col is the tenant column of a tenanted table, and
// whether one applies at all.
//
// §29.2.4 resolves **the tenant column's** Go type — not a second type only
// the tenancy surface sees. Its step 3 exists precisely so that "its spelling
// on the row struct and its spelling in the tenancy context" cannot diverge,
// and steps 1-2 can only honor that by reaching the column too. Applying the
// override at the column's own resolution is what makes the row struct,
// CreateInput, filter, PK struct and TenancyContext agree *by construction*
// rather than by a validation that compares them after the fact.
//
// Leaving it out is what let `tenancy.type: {type: uuid.UUID, import:
// github.com/google/uuid}` emit `WorkspaceID string` on the row struct beside
// `resolvedTenant uuid.UUID`, so the generated `v != resolvedTenant` compared
// two different types and the package did not compile. It also let the import
// the override carries belong to no column, which is how it escaped the
// one-UUID-library-per-package rule — that rule reads *column* claims, so a
// surface no column owns is a surface it cannot see (the mirror of
// `column_map.<col>.type`, which must reach the tenancy context as well as the
// row struct).
//
// The override is spelled non-nullable because a table's tenant column is NOT
// NULL by contract (§29.2.4, enforced in resolveTableTenancy) — the same
// choice resolveTenantColumnType makes, which is what keeps the two identical.
// Detection is read off the config rather than the built TenancyContext
// because these three accessors *are* the detection predicate for an existing
// column: a per-entity `enabled: false` opt-out loses the effective column,
// and every remaining rule in §29.2.3 concerns a column that is absent or
// nullable, neither of which can reach a column builder iterating the column.
func tableTenantTypeOverride(col *parser.Column, tc config.TableConfig, global *config.TenancyConfig) (gotype.GoType, bool) {
	if !tenancyGloballyOn(global) {
		return gotype.GoType{}, false
	}
	if !tenantColumnMatches(col.Name, config.ResolveTableTenancyEnabled(tc, global), config.ResolveTableTenancyColumn(tc, global)) {
		return gotype.GoType{}, false
	}
	return tenantTypeOverrideGoType(config.ResolveTableTenancyType(tc, global), false)
}

// viewTenantTypeOverride is the view analogue of tableTenantTypeOverride.
//
// It differs in exactly one way, and deliberately: the override is spelled at
// the column's *own* nullability rather than non-nullable. §29.2.5 permits a
// nullable tenant column on a view — a LEFT JOIN or an aggregate legitimately
// widens a NOT NULL base column — and the row struct keeps that nullable
// spelling while the tenancy context carries the plain `T` a
// TenantResolver[T] yields (resolveViewTenantColumnType passes nullable=false
// for the same reason). Those two facts are not in conflict, and collapsing
// them would retype a legitimately-nullable view column as non-nullable.
func viewTenantTypeOverride(col *parser.Column, vc config.ViewConfig, global *config.TenancyConfig) (gotype.GoType, bool) {
	if !tenancyGloballyOn(global) {
		return gotype.GoType{}, false
	}
	if !tenantColumnMatches(col.Name, config.ResolveViewTenancyEnabled(vc, global), config.ResolveViewTenancyColumn(vc, global)) {
		return gotype.GoType{}, false
	}
	return tenantTypeOverrideGoType(config.ResolveViewTenancyType(vc, global), col.Nullable)
}

// tenancyGloballyOn mirrors BuildTenancyContext's own guard, and has to be
// checked separately from the per-entity accessors.
//
// `tenancy.enabled` is the master switch: BuildTenancyContext returns a nil map
// the moment it is false, so *no* entity is tenanted and no tenancy code is
// emitted — even for a table carrying `tenancy.enabled: true`. The per-entity
// accessors do not model that, because they answer the narrower question of
// which setting wins for one entity: ResolveTableTenancyEnabled returns the
// per-table `true` without consulting the master switch. Reading them alone
// would retype a tenant column on a package that has no tenancy at all, which
// is the drift this pair of predicates exists to avoid.
func tenancyGloballyOn(global *config.TenancyConfig) bool {
	return global != nil && global.Enabled
}

// tenantColumnMatches reports whether column is the effective tenant column of
// an entity tenancy is enabled for. The name comparison is case-insensitive
// for the reason findTenantColumn gives: unquoted SQL identifiers are
// case-insensitive on all three dialects. An entity with no effective tenant
// column matches nothing.
func tenantColumnMatches(column string, enabled bool, tenantColumn string) bool {
	return enabled && tenantColumn != "" && strings.EqualFold(column, tenantColumn)
}

// checkOverrideAgainstSQLType implements the belt-and-suspenders SQL-type
// sanity check from §29.2.4. The check is deliberately coarse — it catches
// obvious category mismatches (integer column with a UUID override, or a
// uuid column with a numeric override) but tolerates custom wrapper types
// whose names don't reveal their underlying kind.
func checkOverrideAgainstSQLType(override *config.TypeOverride, col *parser.Column) error {
	sqlCat := sqlTypeCategory(col.Type)
	if sqlCat == categoryUnknown {
		return nil
	}
	goCat := goTypeCategory(override.Type, override.Import)
	if goCat == categoryUnknown {
		return nil
	}
	if sqlCat != goCat {
		return fmt.Errorf(
			"tenancy.type %s (import %q) does not match the column's SQL type %q — unify column types, adjust the tenancy.type override, or consolidate via migration",
			override.Type, override.Import, col.Type,
		)
	}
	return nil
}

// Type-category constants shared by the SQL and Go classifiers. Unknown
// means "cannot classify — trust the user".
const (
	categoryUnknown = ""
	categoryInteger = "integer"
	categoryUUID    = "uuid"
	categoryString  = "string"
)

func sqlTypeCategory(sqlType string) string {
	s := strings.ToLower(strings.TrimSpace(sqlType))
	if idx := strings.IndexAny(s, "( "); idx > 0 {
		s = s[:idx]
	}
	s = strings.TrimSuffix(s, "[]")
	switch s {
	case "int", "int2", "int4", "int8",
		"smallint", "integer", "bigint", "tinyint", "mediumint",
		"serial", "smallserial", "bigserial",
		"numeric", "decimal",
		"real", "double", "float", "float4", "float8":
		return categoryInteger
	case "uuid", "uniqueidentifier":
		return categoryUUID
	case "text", "varchar", "char", "name",
		"tinytext", "mediumtext", "longtext",
		"character":
		return categoryString
	}
	return categoryUnknown
}

func goTypeCategory(goType, importPath string) string {
	t := strings.TrimPrefix(goType, "*")
	lower := strings.ToLower(t)

	if strings.Contains(strings.ToLower(importPath), "uuid") {
		return categoryUUID
	}
	if strings.Contains(lower, "uuid") {
		return categoryUUID
	}

	switch t {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"float32", "float64":
		return categoryInteger
	}

	if t == "string" {
		return categoryString
	}
	return categoryUnknown
}

// nonComparableReason reports whether a resolved Go type is structurally
// non-comparable (slice, map, function, or a struct containing one of those
// via `extras:` config). Returns the family that disqualified it so the
// error message is actionable. Unknown types pass — we trust user-defined
// types to be comparable unless the schema declares otherwise.
func nonComparableReason(cfg *config.RootConfig, goType string) (string, bool) {
	t := strings.TrimPrefix(goType, "*")

	if strings.HasPrefix(t, "[]") {
		return "slice types are not comparable", true
	}
	if strings.HasPrefix(t, "map[") {
		return "map types are not comparable", true
	}
	if strings.HasPrefix(t, "func(") || t == "func" {
		return "function types are not comparable", true
	}

	// If the type is a user-declared extra (section 7.9 `extras:` block),
	// look for fields whose declared type would make the struct
	// non-comparable. Field iteration order is unstable (map), so collect
	// and sort for deterministic error output.
	if cfg != nil {
		if extra, ok := lookupExtraType(cfg, t); ok {
			var offenders []string
			for fieldName, field := range extra.Fields {
				if isNonComparableFieldType(field.Type) {
					offenders = append(offenders, fmt.Sprintf("field %q of kind %q", fieldName, field.Type))
				}
			}
			if len(offenders) > 0 {
				sort.Strings(offenders)
				return "struct contains non-comparable " + strings.Join(offenders, ", "), true
			}
		}
	}

	return "", false
}

// lookupExtraType fetches a user-defined extra type by Go type expression.
// Handles the common leading-package-qualifier form ("pkg.Type") as well as
// the bare-name form, since extras are keyed by bare type name in the YAML.
func lookupExtraType(cfg *config.RootConfig, goType string) (config.ExtraType, bool) {
	if extra, ok := cfg.Extras[goType]; ok {
		return extra, true
	}
	if idx := strings.LastIndex(goType, "."); idx >= 0 {
		if extra, ok := cfg.Extras[goType[idx+1:]]; ok {
			return extra, true
		}
	}
	return config.ExtraType{}, false
}

// isNonComparableFieldType matches the top-level check — slices, maps,
// functions make the containing struct non-comparable.
func isNonComparableFieldType(goType string) bool {
	t := strings.TrimPrefix(goType, "*")
	if strings.HasPrefix(t, "[]") {
		return true
	}
	if strings.HasPrefix(t, "map[") {
		return true
	}
	if strings.HasPrefix(t, "func(") || t == "func" {
		return true
	}
	return false
}

// attachTenancyToTables annotates each TableContext with its resolved
// TableTenancyContext from the detection map. A nil tenancyMap means tenancy
// is globally disabled and every TableContext.Tenancy stays nil so templates
// emit non-tenancy code.
func attachTenancyToTables(tables []TableContext, tenancyMap map[string]TenancyContext) {
	if tenancyMap == nil {
		return
	}
	for i := range tables {
		tc := &tables[i]
		key := qualifiedTableName(tc.Schema, tc.TableName)
		entry, ok := tenancyMap[key]
		if !ok {
			// Tenancy enabled globally but this table isn't in the map
			// (shouldn't normally happen) — treat as shared.
			tc.Tenancy = &TableTenancyContext{Tenanted: false}
			continue
		}
		ctx := &TableTenancyContext{
			Tenanted: entry.Tenanted,
			Column:   entry.Column,
			GoType:   entry.GoType,
			Import:   entry.Import,
			Required: entry.Required,
		}
		if entry.Tenanted {
			// Read the field name off the table's resolved columns rather
			// than re-deriving it, so a `column_map.<col>.name` override on
			// the tenant column reaches the tenancy surface — and, through
			// TableTenancyContext.FieldName, the event hook contexts that
			// stamp the tenant.
			ctx.FieldName = columnFieldName(tc.Columns, entry.Column)
			for _, pk := range tc.PKColumns {
				if pk.Name == entry.Column {
					ctx.InPrimaryKey = true
					break
				}
			}
			// Stamp the fact on the columns themselves and re-derive the
			// surfaces that read it. Tenancy is resolved after the table
			// context is assembled, so buildIncrementColumns ran while every
			// column still looked untenanted — the tenant column has to leave
			// the increment surface here or it reaches <T>IncrementColumn
			// (PRD §29.4.2). The resolved Increment flag is the same
			// schema fact (PRD §4.6), so it is re-derived with the columns: a
			// table whose only numeric column is the tenant has no Increment.
			markTenantColumn(tc.Columns, entry.Column)
			markTenantColumn(tc.PKColumns, entry.Column)
			tc.IncrementColumns = buildIncrementColumns(tc.Columns)
			tc.Operations.Increment = len(tc.IncrementColumns) > 0
			// Fold tenant import into the table imports so the
			// resolver / resolver-typed code compiles.
			if entry.Import != "" {
				tc.Imports = UniqueImports(append(tc.Imports, entry.Import))
			}
			tc.Imports = UniqueImports(append(tc.Imports, "github.com/teandresmith/sqlgen/tenancy"))
			// PRD §29.7 Option-2: when the tenant column is part of the
			// composite PK, the caller always supplies it on CreateInput
			// (it's a required PK component; no auto-fill-from-resolver).
			// Runtime verify-match catches caller/resolver mismatches. Skip
			// the omittable promotion so the CreateInput field stays plain.
			if !ctx.InPrimaryKey {
				promoteTenantFieldToOmittable(tc, entry.Column)
			}
		}
		tc.Tenancy = ctx
	}
	annotateO2OChildTenancy(tables, tenancyMap)
}

// attachTenancyToViews annotates each ViewContext with its resolved
// TableTenancyContext from the detection map. The TableTenancyContext type is
// reused rather than forked: its fields are a superset of what a view needs,
// and a second near-identical type would be a second place for the read-path
// shape to drift.
//
// A nil tenancyMap means tenancy is globally disabled and every
// ViewContext.Tenancy stays nil so templates emit non-tenancy code.
//
// InPrimaryKey is never set — a view has no DDL primary key, and the §29.7
// composite-PK rule it drives is a create-path rule views have no analogue for
// (§29.2.5). The omittable promotion attachTenancyToTables performs is skipped
// for the same reason: a view has no CreateInput.
func attachTenancyToViews(views []ViewContext, tenancyMap map[string]TenancyContext) {
	if tenancyMap == nil {
		return
	}
	for i := range views {
		vc := &views[i]
		entry, ok := tenancyMap[qualifiedTableName(vc.Schema, vc.ViewName)]
		if !ok {
			// Tenancy enabled globally but this view isn't in the map
			// (shouldn't normally happen) — treat as shared.
			vc.Tenancy = &TableTenancyContext{Tenanted: false}
			continue
		}
		ctx := &TableTenancyContext{
			Tenanted: entry.Tenanted,
			Column:   entry.Column,
			GoType:   entry.GoType,
			Import:   entry.Import,
			Required: entry.Required,
		}
		if entry.Tenanted {
			// Read the field name off the view's resolved columns rather than
			// re-deriving it, matching the table path.
			ctx.FieldName = columnFieldName(vc.Columns, entry.Column)
			// A view has no increment surface, so nothing is re-derived here —
			// the flag is stamped anyway so a ColumnContext means the same
			// thing whichever entity kind it came off.
			markTenantColumn(vc.Columns, entry.Column)
			markTenantColumn(vc.PKColumns, entry.Column)
			if entry.Import != "" {
				vc.Imports = UniqueImports(append(vc.Imports, entry.Import))
			}
			vc.Imports = UniqueImports(append(vc.Imports, "github.com/teandresmith/sqlgen/tenancy"))
		}
		vc.Tenancy = ctx
	}
}

// annotateO2OChildTenancy populates TenantedO2OChildren / HasTenantedO2OChild
// on each TableContext by scanning every O2OJoinDetail (direct and chained)
// for targets whose TenancyContext is Tenanted. Runs after
// attachTenancyToTables so the tenancy map is the authoritative source — the
// o2o details were built before tenancy was resolved and carried no tenancy
// info of their own (PRD §29.10). When the parent table is tenanted, its
// own tenant filter is handled separately by the existing auto-filter block
// (not here).
func annotateO2OChildTenancy(tables []TableContext, tenancyMap map[string]TenancyContext) {
	if tenancyMap == nil {
		return
	}
	for i := range tables {
		tc := &tables[i]
		var children []TenantedO2OChild
		collectTenantedO2OChildren(tc.O2OJoinDetails, tenancyMap, &children)
		if len(children) == 0 {
			continue
		}
		// Deterministic order: alias-sorted matches the O2OAllTargets
		// convention, so template output is stable across runs.
		sort.Slice(children, func(a, b int) bool { return children[a].Alias < children[b].Alias })
		tc.TenantedO2OChildren = children
		tc.HasTenantedO2OChild = true
		// Ensure the parent has the tenancy import even when the parent
		// itself is not tenanted — resolveTenant needs it. When the parent
		// is itself non-tenanted, also fill in the canonical tenant Go type
		// so the emitted `tenantResolver tenancy.TenantResolver[T]` field +
		// resolveTenant method have a concrete type to refer to. Uniform
		// tenant type across tenanted tables (§29.2.4) guarantees any
		// tenanted entry carries the correct answer.
		tc.Imports = UniqueImports(append(tc.Imports, "github.com/teandresmith/sqlgen/tenancy"))
		if tc.Tenancy == nil || !tc.Tenancy.Tenanted {
			goType, importPath := FirstTenantedType(tenancyMap)
			if tc.Tenancy == nil {
				tc.Tenancy = &TableTenancyContext{Tenanted: false}
			}
			tc.Tenancy.GoType = goType
			tc.Tenancy.Import = importPath
			if importPath != "" {
				tc.Imports = UniqueImports(append(tc.Imports, importPath))
			}
		}
	}
}

// collectTenantedO2OChildren walks the O2O detail tree and appends one
// TenantedO2OChild per tenanted target it discovers.
func collectTenantedO2OChildren(details []O2OJoinDetail, tenancyMap map[string]TenancyContext, out *[]TenantedO2OChild) {
	for _, d := range details {
		key := qualifiedTableName(d.TargetSchema, d.TargetTable)
		if entry, ok := tenancyMap[key]; ok && entry.Tenanted {
			*out = append(*out, TenantedO2OChild{
				Alias:        d.Alias,
				TenantColumn: entry.Column,
			})
		}
		collectTenantedO2OChildren(d.ChainedJoins, tenancyMap, out)
	}
}

// promoteTenantFieldToOmittable ensures the tenant column appears on
// CreateInput / UpdateInput as an omittable.Value[T] rather than a plain
// required field. The runtime emits the auto-set block before any required
// columns are appended, and uses `input.<Field>.Get()` for the mismatch
// check (PRD §29.4.2) — those semantics rely on the field being omittable.
//
// UpdateInput is already omittable for every non-PK column, so this only
// has work to do on CreateInput when buildCreateInputFields classified
// the tenant as required (non-nullable, no default, not an update column).
func promoteTenantFieldToOmittable(tc *TableContext, tenantColumn string) {
	for i := range tc.CreateInputFields {
		f := &tc.CreateInputFields[i]
		if f.ColumnName != tenantColumn || !f.Required {
			continue
		}
		f.Required = false
		f.Omittable = true
		f.GoType, f.Import = wrapOmittable(f.GoType, f.Import)
		break
	}
}

// FirstTenantedType returns the Go type + import of the first tenanted
// table in a tenancy map. Because §29.2.4 requires uniform tenant Go type
// across all tenanted tables, any tenanted entry carries the canonical
// answer. Returns empty strings when tenancyMap is nil, empty, or has no
// tenanted tables (e.g. tenancy enabled but all tables opted-out).
func FirstTenantedType(tenancyMap map[string]TenancyContext) (goType, importPath string) {
	if len(tenancyMap) == 0 {
		return "", ""
	}
	// Deterministic: sort keys so empty-tenanted maps return stably.
	keys := make([]string, 0, len(tenancyMap))
	for k := range tenancyMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if tc := tenancyMap[k]; tc.Tenanted {
			return tc.GoType, tc.Import
		}
	}
	return "", ""
}

// validateUniformTenantType enforces the §29.2.4 invariant that every
// tenanted entity — table or view — resolves to the same Go type. Emits at
// most one error per generate run and names every offending entity + its type
// so the user can fix the set in a single pass.
func validateUniformTenantType(entries []tenantedEntry) error {
	if len(entries) < 2 {
		return nil
	}

	// Group by (goType, import) — an import path difference between two
	// same-named types (gofrs/uuid.UUID vs google/uuid.UUID) is also a
	// divergence we need to flag.
	type key struct{ goType, imp string }
	groups := make(map[key][]string)
	for _, e := range entries {
		k := key{e.goType, e.imp}
		groups[k] = append(groups[k], e.label())
	}
	if len(groups) == 1 {
		return nil
	}

	// Deterministic output: sort groups by Go type then import, and the
	// table list inside each group alphabetically.
	ks := make([]key, 0, len(groups))
	for k := range groups {
		ks = append(ks, k)
	}
	sort.Slice(ks, func(i, j int) bool {
		if ks[i].goType != ks[j].goType {
			return ks[i].goType < ks[j].goType
		}
		return ks[i].imp < ks[j].imp
	})

	var b strings.Builder
	b.WriteString("tenancy: tenanted entities must resolve to a uniform tenant Go type across the generate run; found divergent types: ")
	for i, k := range ks {
		if i > 0 {
			b.WriteString("; ")
		}
		offenders := groups[k]
		sort.Strings(offenders)
		impLabel := k.imp
		if impLabel == "" {
			impLabel = "<stdlib>"
		}
		fmt.Fprintf(&b, "%s (import %s) used by [%s]", k.goType, impLabel, strings.Join(offenders, ", "))
	}
	b.WriteString(" — unify column types, opt one out via tables.<name>.tenancy.enabled: false / views.<name>.tenancy.enabled: false, or consolidate via migration")
	return errors.New(b.String())
}

// finalizeTenancyWiring resolves the tenancy facts that can only be answered
// once every other table pass has run, and is the single place the
// "does this client need a tenant resolver?" union is decided.
//
// Three of its four inputs are set by earlier passes — the table's own
// detection (attachTenancyToTables), its tenanted O2O children
// (annotateO2OChildTenancy) and its tenanted relationship-filter targets
// (wireRelationshipFilters). The fourth, HasTenantedNestedEdge, is readable
// only after wireNestedMutations, because a nested edge's target is resolved
// there. Deciding the union in Go rather than restating it in each template
// is what keeps the emitted field, the emitted helpers and
// applyClientTenancy's assignment list from drifting apart.
//
// It also backfills the metadata a table needs to *spell* the resolver when
// it carries no tenant column of its own — the same backfill
// annotateO2OChildTenancy and backfillRelationshipFilterTenancy perform for
// their own terms, applied here to the union so a term added later inherits
// it. §29.2.4's uniform tenant type across every tenanted entity is what makes
// "any tenanted entity's type" the right answer for a table that has none.
//
// Views are read, not written: a package whose only tenanted entity is a view
// still emits CallOptions.Tenant (§29.2.5 "CallOptions parity"), and the
// relationship loaders below take that field's type as a parameter.
func finalizeTenancyWiring(tables []TableContext, views []ViewContext) {
	goType, importPath := FirstTenantedEntityType(tables, views)

	for i := range tables {
		tc := &tables[i]
		// ExplicitTenantGoType mirrors the condition shared_types.go.tmpl uses
		// to emit CallOptions.Tenant, so a loader that takes the field as a
		// parameter and the field itself appear and disappear together.
		tc.ExplicitTenantGoType = goType

		if goType == "" {
			continue
		}
		if tc.NeedsTenantResolver() {
			tc.Imports = UniqueImports(append(tc.Imports, "github.com/teandresmith/sqlgen/tenancy"))
			if tc.Tenancy == nil {
				tc.Tenancy = &TableTenancyContext{Tenanted: false}
			}
			if !tc.Tenancy.Tenanted {
				tc.Tenancy.GoType, tc.Tenancy.Import = goType, importPath
			}
		}
		// A relationship loader forwards CallOptions.Tenant to the target's own
		// client (PRD §29.4.4), so the type's import is needed wherever the
		// loader is emitted — including on a shared parent that names the type
		// nowhere else.
		if importPath != "" && (tc.NeedsTenantResolver() || tc.HasO2MRelationships) {
			tc.Imports = UniqueImports(append(tc.Imports, importPath))
		}
	}
}
