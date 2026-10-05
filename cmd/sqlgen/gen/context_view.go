package gen

import (
	"errors"
	"slices"
	"strings"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/parser"
)

// BuildViewContexts builds ViewContext values from all inputs.
//
// Errors accumulate across views rather than aborting on the first one, so
// `sqlgen validate` reports every problem in one pass.
func BuildViewContexts(input *GenerateInput, collisions map[string]bool) ([]ViewContext, error) {
	contexts := make([]ViewContext, 0, len(input.Schema.Views))
	var errs []error
	for _, view := range input.Schema.Views {
		vc, err := buildSingleViewContext(input, &view, collisions)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		contexts = append(contexts, vc)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	slices.SortFunc(contexts, func(a, b ViewContext) int {
		if c := strings.Compare(a.Schema, b.Schema); c != 0 {
			return c
		}
		return strings.Compare(a.ViewName, b.ViewName)
	})
	return contexts, nil
}

func buildSingleViewContext(input *GenerateInput, view *parser.View, collisions map[string]bool) (ViewContext, error) {
	cfg := input.Config

	// A regular view can never be concurrently refreshable (design invariant:
	// !Materialized ⇒ !ConcurrentlyRefreshable). The parser upholds this on
	// every discovery path, so a violation here is a hand-built fixture bug.
	if !view.Materialized && view.ConcurrentlyRefreshable {
		panic("view " + view.Name + ": ConcurrentlyRefreshable requires Materialized")
	}

	viewCfg, hasViewCfg := resolveViewConfig(cfg.Views, view.Schema, view.Name)
	structNameOverride := ""
	if hasViewCfg {
		structNameOverride = viewCfg.StructName
	}
	structName := structNameFor(view.Name, view.Schema, structNameOverride, collisions)
	snakeName := snakeNameFor(view.Name, view.Schema, structNameOverride, collisions)

	columns := buildViewColumns(input, view, viewCfg)

	// Detect PK columns (from @pk annotation — columns marked as PrimaryKey).
	var pkColumns []ColumnContext
	for _, col := range columns {
		if col.PrimaryKey {
			pkColumns = append(pkColumns, col)
		}
	}

	compositePK := len(pkColumns) > 1
	compositePKStructName := ""
	if compositePK {
		compositePKStructName = structName + "PK"
	}

	if err := validateResolvedViewFieldNames(qualifiedViewOrName(view), structName, columns, pkColumns, cfg.Input.Dialect); err != nil {
		return ViewContext{}, err
	}

	varName := strings.ToLower(structName[:1])
	filterFields := buildFilterFields(columns, cfg.Input.Dialect, nil)
	scanShapes := buildScanShapes(columns, cfg.Output.Driver, structName)
	imports := UniqueImports(slices.Concat(
		collectColumnImports(columns),
		[]string{"github.com/teandresmith/sqlgen/hook"},
		modelTemplateImports,
		arrayScanImports(cfg, columns),
	))

	cursorDecision := config.ResolveViewConnection(
		viewCfg,
		cfg.Generation,
		qualifiedViewOrName(view),
		columnContextNames(columns),
	)

	return ViewContext{
		StructName:              structName,
		SnakeName:               snakeName,
		ViewName:                view.Name,
		TableName:               view.Name,
		TableNameConstant:       TableConstantName(view.Name, view.Schema, structNameOverride, collisions),
		Schema:                  view.Schema,
		Columns:                 columns,
		PKColumns:               pkColumns,
		HasPK:                   len(pkColumns) > 0,
		CompositePK:             compositePK,
		CompositePKStructName:   compositePKStructName,
		FilterFields:            filterFields,
		AllColumnNames:          buildAllColumnNames(columns),
		VarName:                 varName,
		ScanShapes:              scanShapes,
		Dialect:                 cfg.Input.Dialect,
		Driver:                  cfg.Output.Driver,
		PageSize:                ptrInt(cfg.Generation.PageSize, 100),
		QueryLimit:              ptrInt(cfg.Generation.QueryLimit, 1000),
		CursorKeys:              cursorDecision.Keys,
		HasConnection:           cursorDecision.Emit,
		Imports:                 imports,
		Package:                 cfg.Output.Package,
		Materialized:            view.Materialized,
		ConcurrentlyRefreshable: view.ConcurrentlyRefreshable,
	}, nil
}

// validateResolvedViewFieldNames rejects a view column whose resolved Go field
// name is already declared on the structs the shared templates emit for a view
// (<V>Filter, <V>FieldOptions) — see reserved_fields.go and PRD §8.5.
//
// Views take neither column_map nor exclude_columns, so there is no
// override-gated half here: every view column is checked, and the only escape
// is the view definition itself.
func validateResolvedViewFieldNames(qualifiedView, structName string, columns, pkColumns []ColumnContext, dialect config.Dialect) error {
	var errs []error
	for i := range columns {
		if err := reservedViewFieldError(qualifiedView, structName, columns[i], viewColumnMemberScope(columns[i], pkColumns, dialect)); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// buildViewColumns resolves each view column to a ColumnContext.
//
// Views have no column_map, so every view column resolves to the public
// access role. Resolving explicitly (rather than leaving zero values) keeps
// the ColumnContext invariant: capability booleans are always derived from a
// valid role, so API/manifest consumers never see a spuriously all-false
// column.
//
// viewCfg is threaded in for the one view-level block that can retype a
// column: `views.<name>.tenancy.type` (§29.2.4 steps 1-2, via
// viewTenantTypeOverride). Without it the tenant column reached the row struct
// on its own resolution while the tenancy context carried the override, and a
// view's two spellings of one column disagreed exactly as a table's did.
func buildViewColumns(input *GenerateInput, view *parser.View, viewCfg config.ViewConfig) []ColumnContext {
	accessRole, accessCaps := resolveAccess("")

	columns := make([]ColumnContext, 0, len(view.Columns))
	for _, col := range view.Columns {
		// §29.2.4 order: the tenant column's `tenancy.type` first, then the
		// column's own resolution (step 3, which for a view is the @type
		// literal / aggregate / resolver cascade).
		gt, tenantTyped := viewTenantTypeOverride(&col, viewCfg, input.Config.Tenancy)
		if !tenantTyped {
			gt = viewColumnGoType(input.Resolver, &col, col.Nullable)
		}
		columns = append(columns, ColumnContext{
			Name:       col.Name,
			FieldName:  FieldName(col.Name),
			GoType:     gt.Name,
			Import:     gt.Import,
			ZeroValue:  gt.ZeroValue,
			SQLType:    col.Type,
			Nullable:   col.Nullable,
			PrimaryKey: col.PrimaryKey,
			DBTag:      col.Name,
			JSONTag:    col.Name,
			IsSlice:    gt.IsSlice,
			// Carried for the same reason the table path carries it: a named
			// enum slice (UserRoleSlice) needs its element type so the filter
			// instantiates comparator.Slice[UserRole] rather than the
			// non-comparable slice type, and so the scan shape leaves the
			// named slice's own Scan/Value alone instead of wrapping pq.Array.
			SliceElemType: gt.SliceElemType,
			// Plumb the FK-stringification method (as tables do) so a PK column
			// whose Go type is a stringifiable identifier (e.g. uuid.UUID via a
			// type override) resolves to comparator.ID in the generated filter —
			// matching the view's Get(pk) path — rather than defaulting to
			// comparator.String and producing a non-compiling filter struct.
			FKConvert: gt.FKConvert,

			Access:             accessRole,
			APIReadable:        accessCaps.APIReadable,
			APIWritable:        accessCaps.APIWritable,
			APIFilterable:      accessCaps.APIFilterable,
			APISortable:        accessCaps.APISortable,
			EventRedacted:      accessCaps.EventRedacted,
			ManifestVisibility: accessCaps.ManifestVisibility,
		})
	}
	return columns
}

// viewColumnGoType resolves one view column to its Go type. Views carry no
// column_map / type_map block, so the cascade is shorter than the table one:
// an @type annotation (or aggregate-inferred) literal wins, then the
// aggregate-aware path (SUM's dialect widening, ARRAY_AGG's array element
// resolution), then the plain SQL-to-Go resolver.
//
// nullable is a parameter rather than read off col because the tenancy path
// needs the value spelling of a column the row struct spells as nullable —
// see resolveViewTenantColumnType. Every other caller passes col.Nullable.
func viewColumnGoType(resolver *gotype.Resolver, col *parser.Column, nullable bool) gotype.GoType {
	switch {
	case col.GoTypeLiteral != "":
		// Column type was pre-resolved to a Go type by @type annotation or
		// aggregate inference — use it directly instead of the SQL-to-Go resolver.
		gt := gotype.FromLiteral(col.GoTypeLiteral, nullable)
		if col.GoTypeImport != "" {
			gt.Import = col.GoTypeImport
		}
		return gt
	case col.Aggregate == "ARRAY_AGG":
		// Resolves the array SQL type the parser recorded (e.g. text[]) to a
		// Go slice, which carries IsSlice and therefore the array scan shape.
		return resolver.ResolveAggregate(col.Aggregate, col.Type, nullable, col.Name, nil, nil)
	case col.Aggregate == "SUM":
		// SUM widens its result type (e.g. SUM(integer) → bigint in
		// PostgreSQL); resolve through the widening-aware path so the scan
		// target matches the value the database returns.
		return resolver.ResolveAggregate(col.Aggregate, col.Type, nullable, col.Name, nil, nil)
	default:
		return resolver.Resolve(col.Type, nullable, col.Name, nil, nil)
	}
}

// qualifiedViewOrName returns the schema-qualified view name, or the bare
// name if the schema is empty.
func qualifiedViewOrName(v *parser.View) string {
	if v.Schema == "" {
		return v.Name
	}
	return v.Schema + "." + v.Name
}

func ptrInt(p *int, def int) int {
	if p != nil {
		return *p
	}
	return def
}
