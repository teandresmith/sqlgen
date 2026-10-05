package gen

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/parser"
)

// BuildTableContexts builds TableContext values from all inputs.
//
// Tables matching any pattern in cfg.ExcludeTables (PRD §6.4) are dropped
// silently before table-context build. The filter applies to file inputs
// and introspected schemas alike.
//
// Tables that have no primary key after override resolution are skipped
// entirely (PRD §9.4b). The corresponding warning is emitted earlier from
// config.ValidatePostParse::validateMissingPrimaryKey; no client, model,
// cache entry, or event hook is generated. Users can either declare a PK
// via tables.<name>.primary_key.columns or drop the table via the
// top-level exclude_tables filter to silence the warning.
func BuildTableContexts(input *GenerateInput, collisions map[string]bool) ([]TableContext, error) {
	applyConfigDeclaredFKs(input.Schema, input.Config)
	input.relTargets = newRelationshipTargets(input, collisions)
	excludePatterns := input.Config.ExcludeTables
	contexts := make([]TableContext, 0, len(input.Schema.Tables))
	// Errors accumulate across tables rather than aborting on the first one:
	// `sqlgen validate` reaches this path and reports every problem
	// in one pass, and `generate` gains the same batch report for free.
	var errs []error
	for _, table := range input.Schema.Tables {
		if _, matched := config.MatchExcludeTablePattern(table.Name, table.Schema, excludePatterns); matched {
			continue
		}
		if !tableHasResolvedPK(input.Config, &table) {
			continue
		}
		tc, err := buildSingleTableContext(input, &table, collisions)
		if err != nil {
			errs = append(errs, fmt.Errorf("building context for table %s: %w", table.Name, err))
			continue
		}
		contexts = append(contexts, tc)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	slices.SortFunc(contexts, func(a, b TableContext) int {
		if c := strings.Compare(a.Schema, b.Schema); c != 0 {
			return c
		}
		return strings.Compare(a.TableName, b.TableName)
	})

	// Second pass: wire FK Go types and FK column nullability into relationships
	// using cross-table information (PK and column metadata).
	wireRelationshipFKMetadata(contexts)

	// Third pass: resolve the junction clients and the nested-surface flag the
	// entity clients carry for nested mutations (PRD §9.9). Cross-table for the
	// same reason as the second pass — the junction's struct name belongs to
	// the junction's own context.
	wireNestedMutationSurface(contexts)

	return contexts, nil
}

// wireRelationshipFKMetadata resolves the FKGoType, FKNullable, FKColumnGoType
// and FKFieldName fields on each relationship by looking up cross-table data
// from the built contexts.
//
// FKGoType comes from the parent/target table's PK Go type. FKNullable comes
// from the FK column on the target table (O2M/O2O) or the junction's local FK
// column (M2M); it is the nullability the *target* table's filter struct will
// expect for the FK field, which determines whether relationship loaders must
// emit `comparator.NullableNumber`/`comparator.NullableID` instead of the
// non-nullable variants. FKColumnGoType is the resolved Go type
// of the FK column itself, which may be a Null-wrapper struct.
// FKFieldName is that column's Go field name, which the O2M loader spells on
// the *target* struct (`filter.<FKFieldName>`, `r.<FKFieldName>`) and which
// therefore has to come from the target table's resolved columns rather than
// from the FK column name.
func wireRelationshipFKMetadata(contexts []TableContext) {
	// Both lookups are keyed by schema.table, or by the bare table name in a
	// dialect without schemas — the key lookupPKColumn and lookupTableCols
	// read, since every edge carries its schema.
	//
	// Build lookup: table → first PK column. One map rather than one per fact
	// for the same reason colsByTable is one map: the Go type and the Go field
	// name both come off the same column context, and parallel maps drift.
	pkColumns := make(map[string]ColumnContext, len(contexts))
	// Build lookup: table → column name → column. One map rather than one per
	// field: every FK fact the wiring needs (nullability, Go type, Go field
	// name) is already resolved on the column context, and parallel maps drift.
	colsByTable := make(map[string]map[string]ColumnContext, len(contexts))
	for _, tc := range contexts {
		key := qualifiedOrBare(tc.Schema, tc.TableName)
		if len(tc.PKColumns) > 0 {
			pkColumns[key] = tc.PKColumns[0]
		}
		byName := make(map[string]ColumnContext, len(tc.Columns))
		for _, c := range tc.Columns {
			byName[c.Name] = c
		}
		colsByTable[key] = byName
	}

	for i := range contexts {
		tc := &contexts[i]
		wireRelListFKMetadata(tc.O2MRelationships, tc, pkColumns, colsByTable)
		wireRelListFKMetadata(tc.M2MRelationships, tc, pkColumns, colsByTable)
		wireRelListFKMetadata(tc.O2ORelationships, tc, pkColumns, colsByTable)
		wireRelListFKMetadata(tc.Relationships, tc, pkColumns, colsByTable)
	}
}

func wireRelListFKMetadata(rels []RelationshipContext, parent *TableContext, pkColumns map[string]ColumnContext, colsByTable map[string]map[string]ColumnContext) {
	for j := range rels {
		wireRelFKMetadata(&rels[j], parent, pkColumns, colsByTable)
	}
}

// wireRelFKMetadata fills FKGoType, FKNullable, FKColumnGoType, FKFieldName
// and TargetPKFieldName on a single relationship by consulting the cross-table
// lookup. Split out to keep the per-relationship-type branching readable.
func wireRelFKMetadata(r *RelationshipContext, parent *TableContext, pkColumns map[string]ColumnContext, colsByTable map[string]map[string]ColumnContext) {
	if r.FKGoType == "" {
		r.FKGoType = resolveFKGoType(r, parent, pkColumns)
	}

	fkCol := r.FKColumn
	switch r.Type {
	case parser.OneToMany:
		// O2M: the FK column lives on the target (many) table for both
		// auto-detected (addO2M) and config edges. This is the only path that
		// actually consumes FKNullable today (the get.go.tmpl relationship
		// loader picks a nullable comparator off it).
		wireRelFKFromTable(r, fkCol, r.TargetSchema, r.TargetTable, colsByTable)
	case parser.OneToOne:
		// O2O FK placement depends on the edge's origin: auto-detected
		// (addO2O) edges carry the FK on the *source*/parent table, while
		// config edges (e.g. `fk: entity_id` referencing the target) carry it
		// on the *target*. Resolve against whichever table actually holds the
		// column so FKNullable / FKColumnGoType are correct for both shapes —
		// target first (config shape), then parent (auto-detected shape).
		if !wireRelFKFromTable(r, fkCol, r.TargetSchema, r.TargetTable, colsByTable) {
			wireRelFKFromTable(r, fkCol, parent.Schema, parent.TableName, colsByTable)
		}
	case parser.ManyToMany:
		fkCol = r.JunctionLocalFK
		wireRelFKFromTable(r, fkCol, r.JunctionSchema, r.JunctionTable, colsByTable)
		// The M2M loader keys its target-to-parent map on the *target's* PK
		// field and declares that column on the target fetch, so both
		// spellings have to come from the target table rather than the parent's
		// PK.
		if pk, ok := lookupPKColumn(pkColumns, r.TargetSchema, r.TargetTable); ok {
			r.TargetPKFieldName = pk.FieldName
			r.TargetPKColumn = pk.Name
		}
	}

	// Final fallback for the field name: a bare table (unit fixture, or a
	// target outside the generated set) leaves it unresolved, in which case
	// the naming engine's spelling is the only answer available and matches
	// what an un-overridden column would have produced anyway.
	if r.FKFieldName == "" && fkCol != "" {
		r.FKFieldName = FieldName(fkCol)
	}

	// Final fallback: FKColumnGoType matches FKGoType when no cross-table
	// metadata was available (covers unit fixtures and bare tables).
	if r.FKColumnGoType == "" {
		r.FKColumnGoType = r.FKGoType
	}

	// Final fallback for the target PK: with the target outside the generated
	// set the parent's own PK spelling is the only answer available, and it is
	// what the M2M loader assumed before the cross-table lookup existed.
	if r.TargetPKFieldName == "" && len(parent.PKColumns) > 0 {
		r.TargetPKFieldName = parent.PKColumns[0].FieldName
		r.TargetPKColumn = parent.PKColumns[0].Name
	}
}

// wireRelFKFromTable resolves FKNullable, FKFieldName and (when still unset)
// FKColumnGoType for r by reading fkCol off the table identified by
// (schema, name). It reports whether that table actually carries fkCol, so
// O2O callers can fall back to the parent table when the FK lives on the
// source side (auto-detected addO2O edges) rather than the target. Returns
// false — leaving r untouched — when the table is absent from the lookup or
// does not declare fkCol.
func wireRelFKFromTable(r *RelationshipContext, fkCol, schema, name string, colsByTable map[string]map[string]ColumnContext) bool {
	cols := lookupTableCols(colsByTable, schema, name)
	if cols == nil {
		return false
	}
	col, ok := cols[fkCol]
	if !ok {
		return false
	}
	r.FKNullable = col.Nullable
	r.FKFieldName = col.FieldName
	if r.FKColumnGoType == "" {
		r.FKColumnGoType = col.GoType
	}
	return true
}

// resolveFKGoType returns the FK Go type for a relationship: the parent's PK
// type for O2M/O2O (the FK column on the target matches the parent's PK), and
// the target table's PK type for M2M.
func resolveFKGoType(r *RelationshipContext, parent *TableContext, pkColumns map[string]ColumnContext) string {
	switch r.Type {
	case parser.OneToMany, parser.OneToOne:
		if len(parent.PKColumns) > 0 {
			return parent.PKColumns[0].GoType
		}
	case parser.ManyToMany:
		// By the target's schema, as the TargetPKColumn wiring reads it.
		if pk, ok := lookupPKColumn(pkColumns, r.TargetSchema, r.TargetTable); ok {
			return pk.GoType
		}
	}
	return "string"
}

// lookupPKColumn resolves a table's first PK column from the cross-table
// lookup by the schema the edge carries — the rule lookupTableCols applies.
func lookupPKColumn(pkColumns map[string]ColumnContext, schema, name string) (ColumnContext, bool) {
	col, ok := pkColumns[qualifiedOrBare(schema, name)]
	return col, ok
}

// lookupTableCols resolves a table's columns by the schema the edge carries.
// Every edge carries its target's and its junction's schema whenever the
// dialect has one, so a schema-qualified miss is a table
// outside the generated set — an excluded junction — and never a reason to read
// another schema's table of the same name. The bare name is the key only
// without a schema.
func lookupTableCols(colsByTable map[string]map[string]ColumnContext, schema, name string) map[string]ColumnContext {
	return colsByTable[qualifiedOrBare(schema, name)]
}

func buildSingleTableContext(input *GenerateInput, table *parser.Table, collisions map[string]bool) (TableContext, error) {
	cfg := input.Config
	tableCfg := resolveTableConfig(cfg.Tables, table.Schema, table.Name)

	if err := validateColumnTypeLiterals(qualifiedTableOrName(table), tableCfg); err != nil {
		return TableContext{}, err
	}

	structName := structNameFor(table.Name, table.Schema, tableCfg.StructName, collisions)

	columns, pkColumns := buildTableColumns(input, table, tableCfg)

	input.Warnings = append(input.Warnings,
		incrementSQLTypeWarnings(qualifiedTableOrName(table), tableCfg, columns)...)

	pkStrategy := detectPKStrategy(input, table, pkColumns, tableCfg)

	softDelete, err := detectTableSoftDelete(table, cfg, columns)
	if err != nil {
		return TableContext{}, fmt.Errorf("detecting soft delete for %s: %w", table.Name, err)
	}

	updateColumns := parser.DetectUpdateColumns(table, cfg.Generation.UpdateColumns)

	return assembleTableContext(input, table, tableCfg, structName, columns, pkColumns,
		pkStrategy, softDelete, updateColumns, collisions)
}

func buildTableColumns(input *GenerateInput, table *parser.Table, tableCfg config.TableConfig) ([]ColumnContext, []ColumnContext) {
	var tableOverrides map[string]config.TypeOverride
	if tableCfg.Overrides != nil {
		tableOverrides = tableCfg.Overrides.Types
	}

	excludeCols := config.ResolveTableExcludeColumns(tableCfg, input.Config.Generation)
	excludeSet := make(map[string]bool, len(excludeCols))
	for _, c := range excludeCols {
		excludeSet[c] = true
	}

	columns := make([]ColumnContext, 0, len(table.Columns))
	for _, col := range table.Columns {
		if excludeSet[col.Name] {
			continue
		}
		columns = append(columns, buildColumnContext(input, &col, tableCfg, tableOverrides))
	}

	// PK column override (PRD §8.6 — tables.<name>.primary_key.columns):
	// when present, silently replace the auto-detected PK set with the
	// declared columns in declaration order. Pre-parse validation
	// (validatePrimaryKeyOverride) hard-errors on missing or nullable
	// columns, so by the time we get here the override is well-formed.
	if tableCfg.PrimaryKey != nil && len(tableCfg.PrimaryKey.Columns) > 0 {
		overrideSet := make(map[string]bool, len(tableCfg.PrimaryKey.Columns))
		for _, name := range tableCfg.PrimaryKey.Columns {
			overrideSet[name] = true
		}
		for i := range columns {
			columns[i].PrimaryKey = overrideSet[columns[i].Name]
		}
		// Build pkColumns in the user-declared order. Order is load-bearing
		// for composite-PK cache key construction (CACHE.md §16) and the
		// generated <Table>PK struct field order.
		colByName := make(map[string]ColumnContext, len(columns))
		for _, c := range columns {
			colByName[c.Name] = c
		}
		pkColumns := make([]ColumnContext, 0, len(tableCfg.PrimaryKey.Columns))
		for _, name := range tableCfg.PrimaryKey.Columns {
			if col, ok := colByName[name]; ok {
				pkColumns = append(pkColumns, col)
			}
		}
		return columns, pkColumns
	}

	var pkColumns []ColumnContext
	for i := range columns {
		if columns[i].PrimaryKey {
			pkColumns = append(pkColumns, columns[i])
		}
	}
	return columns, pkColumns
}

func detectTableSoftDelete(table *parser.Table, cfg *config.RootConfig, columns []ColumnContext) (*SoftDeleteContext, error) {
	sdCol, sdStrategy, err := parser.DetectSoftDelete(table, softDeleteColumnNames(cfg.Generation.SoftDeleteColumns))
	if err != nil {
		return nil, fmt.Errorf("detecting soft delete: %w", err)
	}
	if sdCol == "" {
		return nil, nil
	}
	return &SoftDeleteContext{
		Column:    sdCol,
		FieldName: columnFieldName(columns, sdCol),
		Strategy:  config.SoftDeleteType(sdStrategy),
	}, nil
}

// modelTemplateImports is the import set the table and view templates always
// reference but no column ever declares: the standard-library packages the
// emitted scan, filter and cursor code reaches for, plus the four sqlgen
// runtime packages every model file binds.
//
// Declaring them is what keeps generation off goimports' module-cache walk.
// imports.Process resolves against assumed package names first and returns
// before it ever constructs a resolver when the declared imports already
// satisfy every reference in the file. A missing standard-library path stays
// cheap — goimports fills it from a baked-in table — but a missing non-stdlib
// path (sql, database, comparator, omittable) sends it through a full scan of
// GOMODCACHE, which cost ~3s per emitted file and 97% of all generation time.
// With the set declared, generation no longer scales with the size
// of the consumer's module cache.
//
// The list is deliberately a superset. A table whose columns need no base64
// cursor, no iterator and no omittable field still declares them, and the
// Format pass that follows prunes whatever the file does not use, so an entry
// that goes unused costs nothing while a missing one costs seconds. That is
// the same union-then-prune the file_per_table layout already relies on
// (generateTablesPerFile), and it is why completeness rather than exactness is
// the invariant TestGeneratedImportsAreDeclared enforces.
var modelTemplateImports = []string{
	"context",
	"encoding/base64",
	"encoding/json",
	"errors",
	"fmt",
	"iter",
	"slices",
	"strconv",
	"time",
	"github.com/teandresmith/sqlgen/comparator",
	"github.com/teandresmith/sqlgen/database",
	"github.com/teandresmith/sqlgen/omittable",
	"github.com/teandresmith/sqlgen/sql",
}

// tableImports resolves the import set for one table's generated files: the
// column-driven imports, the template baseline, plus the two driver-conditional
// additions.
func tableImports(cfg *config.RootConfig, columns []ColumnContext) []string {
	imports := append(collectColumnImports(columns), "github.com/teandresmith/sqlgen/hook")
	imports = append(imports, modelTemplateImports...)
	// When driver is pgx, add the v5 import explicitly so goimports doesn't
	// resolve pgx.Batch to the unversioned github.com/jackc/pgx (v3).
	if cfg.Output.Driver == config.DriverPgx {
		imports = append(imports, "github.com/jackc/pgx/v5")
	}
	imports = append(imports, arrayScanImports(cfg, columns)...)
	return UniqueImports(imports)
}

// arrayScanImports returns the imports a generated file needs for the array
// scan wrappers buildSingleScanShape emits, or nil when it emits none.
//
// Tables and views share buildSingleScanShape, so they share this: when the
// driver is stdlib and a column is a bare slice (e.g., text[]), the scan
// target is pq.Array(...) and the file needs lib/pq. This only applies to
// PostgreSQL array columns — MySQL and SQLite have no array types.
//
// Declaring the import rather than leaving goimports to infer it is what makes
// the emitted file self-contained: goimports resolves `pq` by scanning the
// module cache, so a consumer whose cache has never held lib/pq gets a file
// that references pq with no import at all.
func arrayScanImports(cfg *config.RootConfig, columns []ColumnContext) []string {
	if cfg.Input.Dialect == config.DialectPostgres &&
		cfg.Output.Driver == config.DriverStdlib &&
		hasBareSliceColumn(columns) {
		return []string{"github.com/lib/pq"}
	}
	return nil
}

// buildTableRelationships resolves a table's relationships, records their
// warnings, and checks the resulting field names against the columns'.
func buildTableRelationships(
	input *GenerateInput, table *parser.Table, tableCfg config.TableConfig,
	structName string, columns, pkColumns []ColumnContext,
) ([]RelationshipContext, error) {
	rels, warnings, err := buildRelationshipContexts(input.Schema, table, tableCfg, input.relTargets)
	if err != nil {
		return nil, err
	}
	input.Warnings = append(input.Warnings, warnings...)
	if err := validateResolvedFieldNames(qualifiedTableOrName(table), structName, columns, pkColumns, rels, input.Config.Input.Dialect); err != nil {
		return nil, err
	}
	return rels, nil
}

func assembleTableContext(
	input *GenerateInput, table *parser.Table, tableCfg config.TableConfig,
	structName string, columns, pkColumns []ColumnContext, pkStrategy config.PKStrategy,
	softDelete *SoftDeleteContext, updateColumns []string,
	collisions map[string]bool,
) (TableContext, error) {
	cfg := input.Config

	compositePK := len(pkColumns) > 1
	compositePKStructName := ""
	if compositePK {
		compositePKStructName = structName + "PK"
	}

	cursorDecision := config.ResolveTableConnection(
		tableCfg,
		cfg.Generation,
		qualifiedTableOrName(table),
		columnContextNames(columns),
		columnContextNames(pkColumns),
	)

	imports := tableImports(cfg, columns)
	rels, err := buildTableRelationships(input, table, tableCfg, structName, columns, pkColumns)
	if err != nil {
		return TableContext{}, err
	}
	o2oRels, o2mRels, m2mRels := classifyRelationships(rels)

	// loadRelationships is emitted under the same predicate TableContext
	// publishes as HasO2MRelationships, and it is the only generated code that
	// reaches for errgroup. Declaring the import here rather than leaving
	// goimports to infer it keeps the emitted file self-contained — goimports
	// resolves `errgroup` by scanning the module cache, so a consumer whose
	// cache has never held golang.org/x/sync gets a file that references
	// errgroup with no import at all.
	if len(o2mRels) > 0 || len(m2mRels) > 0 {
		imports = UniqueImports(append(imports, "golang.org/x/sync/errgroup"))
	}

	varName := strings.ToLower(structName[:1])
	var o2oJoinDetails, o2oAllTargets, o2oAssignOrder []O2OJoinDetail
	var o2oParentScanCases []O2OScanCase
	if len(o2oRels) > 0 {
		var err error
		o2oJoinDetails, o2oAllTargets, err = buildO2OJoinDetails(input, o2oRels, varName, pkColumns[0].Name)
		if err != nil {
			return TableContext{}, fmt.Errorf("building o2o join details for %s: %w", table.Name, err)
		}
		o2oAssignOrder = reverseO2OTargets(o2oAllTargets)
		o2oParentScanCases = buildO2OParentScanCases(columns, varName, varName, cfg.Output.Driver)
	}

	resolvedOps := toResolvedOperations(softDelete, columns)
	if !cursorDecision.Emit {
		resolvedOps.Connection = false
	}
	conflictTargets := buildConflictTargets(table, tableCfg, structName)
	omitUpsertWithoutConflictTarget(&resolvedOps, conflictTargets)

	return TableContext{
		StructName:                structName,
		SnakeName:                 snakeNameFor(table.Name, table.Schema, tableCfg.StructName, collisions),
		TableName:                 table.Name,
		TableNameConstant:         TableConstantName(table.Name, table.Schema, tableCfg.StructName, collisions),
		Schema:                    table.Schema,
		Description:               resolveTableDescription(table, tableCfg),
		Columns:                   columns,
		PKColumns:                 pkColumns,
		PKDeclaredInSchema:        len(pkColumns) > 0 && pkDeclaredInSchema(table, tableCfg, columnContextNames(pkColumns)),
		PKStrategy:                pkStrategy,
		UUIDVersion:               resolveUUIDVersion(tableCfg, cfg),
		CompositePK:               compositePK,
		CompositePKStructName:     compositePKStructName,
		SoftDelete:                softDelete,
		ExcludeDeleted:            resolveExcludeDeleted(cfg.Generation.ExcludeDeleted, softDelete),
		UpdateColumns:             buildUpdateColumnContexts(updateColumns, columns),
		Relationships:             rels,
		Operations:                resolvedOps,
		ConflictTargets:           conflictTargets,
		IncrementColumns:          buildIncrementColumns(columns),
		FilterFields:              buildFilterFields(columns, cfg.Input.Dialect, softDelete),
		RelationshipOptionsDefs:   buildRelationshipOptionsDefs(rels),
		RelationshipTargetClients: buildRelationshipTargetClients(o2mRels, m2mRels),
		CreateInputFields:         buildCreateInputFields(columns, pkStrategy, updateColumns, softDelete),
		UpdateInputFields:         buildUpdateInputFields(columns, updateColumns, softDelete),
		ScanShapes:                buildScanShapes(columns, cfg.Output.Driver, structName),
		AllColumnNames:            buildAllColumnNames(columns),
		VarName:                   varName,
		HasO2ORelationships:       len(o2oRels) > 0,
		HasO2MRelationships:       len(o2mRels) > 0 || len(m2mRels) > 0,
		O2ORelationships:          o2oRels,
		O2MRelationships:          o2mRels,
		M2MRelationships:          m2mRels,
		O2OJoinDetails:            o2oJoinDetails,
		O2OAllTargets:             o2oAllTargets,
		O2OAssignOrder:            o2oAssignOrder,
		O2OParentScanCases:        o2oParentScanCases,
		Dialect:                   cfg.Input.Dialect,
		Driver:                    cfg.Output.Driver,
		BatchSize:                 config.ResolveTableBatchSize(tableCfg, cfg.Generation),
		PageSize:                  config.ResolveTablePageSize(tableCfg, cfg.Generation),
		QueryLimit:                config.ResolveTableQueryLimit(tableCfg, cfg.Generation),
		CursorKeys:                cursorDecision.Keys,
		StrictUpdates:             config.ResolveTableStrictUpdates(tableCfg, cfg.Generation),
		Imports:                   imports,
		Package:                   cfg.Output.Package,
	}, nil
}

// validateResolvedFieldNames rejects a resolved Go field name that cannot
// coexist with the rest of the generated entity. Two rules, with two different
// gates (PRD §8.5):
//
//   - **Reserved names**, checked on every column and relationship: the name
//     is already declared by the templates on a struct the field lands on
//     (<T>Filter, <T>FieldOptions, Update<T>Item) or by the increment enum
//     type. See reserved_fields.go — that collision can never produce
//     compiling code, so it is reported whether or not an override caused it.
//   - **Duplicate names**, checked only when an override introduced the
//     duplicate: another column's resolved name or a relationship field, both
//     of which land on the same struct (templates/table/model.go.tmpl). A
//     schema that already produces two identical field names without any
//     override is a pre-existing condition this validation deliberately does
//     not start failing; `go build` on the generated package still catches it.
//   - **Duplicate relationship fields**, checked on every pair and gated on
//     nothing: two relationships resolving to one Go field is unconditionally
//     non-compiling, so the override gate above would only hide it. This is
//     the §13.7.3 row 2 rule ("the Go field names on <Table>FieldOptions must
//     be distinct").
//
// Without these checks the collision surfaces as a duplicate-field or
// duplicate-method compile error inside generated code rather than as an
// actionable config error.
func validateResolvedFieldNames(
	qualifiedTable, structName string,
	columns, pkColumns []ColumnContext,
	rels []RelationshipContext,
	dialect config.Dialect,
) error {
	var reservedErrs []error
	for i := range columns {
		if err := reservedColumnFieldError(qualifiedTable, structName, columns[i], columnMemberScope(columns[i], pkColumns, dialect)); err != nil {
			reservedErrs = append(reservedErrs, err)
		}
	}
	for i := range rels {
		if err := reservedRelationshipFieldError(qualifiedTable, structName, rels[i]); err != nil {
			reservedErrs = append(reservedErrs, err)
		}
	}

	if err := validateDuplicateRelationshipFields(qualifiedTable, rels); err != nil {
		reservedErrs = append(reservedErrs, err)
	}
	if err := validateDuplicateFieldNames(qualifiedTable, columns, rels); err != nil {
		reservedErrs = append(reservedErrs, err)
	}
	return errors.Join(reservedErrs...)
}

// validateDuplicateRelationshipFields rejects two relationships on one table
// resolving to the same Go field (PRD §4.8, §13.7.3 row 2). It is ungated
// where validateDuplicateFieldNames is override-gated, because this pair can
// never produce compiling code: the field lands twice on the entity struct and
// twice on <Table>FieldOptions, so there is no config under which it is the
// intent. The override gate would only delay the report to `go build` inside
// generated code, where validate and generate both report success and the
// build fails.
//
// A surviving pair is always same-provenance, so neither side is privileged
// and the message names both symmetrically. A declared-vs-inferred collision
// cannot reach here — buildRelationshipContexts resolves that one by
// replacement (§13.4). What remains is two declared entries whose
// names pascalize onto one field, which config.validateRelationships misses
// because it compares `name:` byte-equal (`primary_document` and
// `PrimaryDocument` are distinct names, one field), or two inferred edges to
// different targets whose names pluralize onto one.
//
// rels is sorted by Name before this runs, so the reported pair is stable.
func validateDuplicateRelationshipFields(qualifiedTable string, rels []RelationshipContext) error {
	seen := make(map[string]string, len(rels))
	var errs []error
	for i := range rels {
		prev, taken := seen[rels[i].FieldName]
		if !taken {
			seen[rels[i].FieldName] = rels[i].Name
			continue
		}
		errs = append(errs, fmt.Errorf(
			"tables.%s.relationships: relationships %q and %q both resolve to the Go field %q — two relationships on one table cannot share a Go field name (PRD §13.7.3); rename one, or drop an auto-detected edge with `exclude_relationships`",
			qualifiedTable, prev, rels[i].Name, rels[i].FieldName,
		))
	}
	return errors.Join(errs...)
}

// validateDuplicateFieldNames is the override-gated half of the rule: two
// fields on the same entity struct resolving to one Go name.
func validateDuplicateFieldNames(qualifiedTable string, columns []ColumnContext, rels []RelationshipContext) error {
	overridden := false
	for i := range columns {
		if columns[i].FieldNameOverridden {
			overridden = true
			break
		}
	}
	if !overridden {
		return nil
	}

	type owner struct {
		sqlName    string
		kind       string
		overridden bool
	}
	seen := make(map[string]owner, len(columns)+len(rels))
	var errs []error
	claim := func(fieldName string, o owner) {
		prev, taken := seen[fieldName]
		if !taken {
			seen[fieldName] = o
			return
		}
		if !prev.overridden && !o.overridden {
			return
		}
		errs = append(errs, fmt.Errorf(
			"tables.%s.column_map: Go field name %q is claimed by both %s %q and %s %q — rename one of them",
			qualifiedTable, fieldName, prev.kind, prev.sqlName, o.kind, o.sqlName,
		))
	}

	for i := range columns {
		claim(columns[i].FieldName, owner{
			sqlName:    columns[i].Name,
			kind:       "column",
			overridden: columns[i].FieldNameOverridden,
		})
	}
	for i := range rels {
		claim(rels[i].FieldName, owner{sqlName: rels[i].Name, kind: "relationship"})
	}
	return errors.Join(errs...)
}

// validateColumnTypeLiterals rejects a per-column Go type literal whose
// package could never be imported. Two shapes qualify:
//
//   - a `type_map` value that is package-qualified and not one gotype already
//     knows. `type_map` has no `import` field, so the generated file would
//     reference a package nothing imports — it compiles only by accident,
//     when an unrelated column happens to pull the same package in;
//   - a `column_map.<col>.type` in the same position whose sibling `.import`
//     is unset.
//
// This is the division of labour §8.5 documents: `type_map` is shorthand for
// builtins and same-package types, and `column_map.type` + `.import` is the
// escape hatch for anything external.
//
// It lives here rather than in config.ValidatePreParse because the registry
// it consults is gotype's, and gotype imports config — the reverse edge would
// be a cycle. Same reason validateResolvedFieldNames sits here.
func validateColumnTypeLiterals(qualifiedTable string, tableCfg config.TableConfig) error {
	var errs []error
	for _, column := range slices.Sorted(maps.Keys(tableCfg.TypeMap)) {
		literal := tableCfg.TypeMap[column]
		if !needsExplicitImport(literal) {
			continue
		}
		errs = append(errs, fmt.Errorf(
			"tables.%s.type_map.%s: %q names a package-qualified type but type_map cannot carry an import — move it to tables.%s.column_map.%s.type with an explicit .import",
			qualifiedTable, column, literal, qualifiedTable, column,
		))
	}
	for _, column := range slices.Sorted(maps.Keys(tableCfg.ColumnMap)) {
		override := tableCfg.ColumnMap[column]
		if override.Import != "" || !needsExplicitImport(override.Type) {
			continue
		}
		errs = append(errs, fmt.Errorf(
			"tables.%s.column_map.%s.type: %q names a package-qualified type — set tables.%s.column_map.%s.import to the package it comes from",
			qualifiedTable, column, override.Type, qualifiedTable, column,
		))
	}
	return errors.Join(errs...)
}

// baseSQLType follows domain definitions to the underlying base SQL type, so a
// classifier sees `text` where the column says `email`. Falls back to the raw
// type when no resolver is available (hand-built unit fixtures).
func baseSQLType(resolver *gotype.Resolver, sqlType string) string {
	if resolver == nil {
		return sqlType
	}
	return resolver.BaseSQLType(sqlType)
}

// incrementSQLTypeWarnings reports the columns dropped from the Increment
// surface by the arithmetic rule, one message each (PRD §7.1, §8.2).
//
// Increment eligibility keys on the resolved Go type, and a per-column
// override replaces that type without restating what the column is to the
// database. Retyping a `text` column to `int32` or `decimal.Decimal` would
// otherwise emit the whole increment surface for it — the enum constant, the
// `_inc` / `_dec` operators on the update input, and `SET "col" = "col" + $1`.
// What the database then does is dialect-specific and never what was meant:
// PostgreSQL errors (`operator does not exist: text + integer`), MySQL
// coerces, and SQLite silently rewrites the stored value through binary float
// (`'2026-08-28' + 1` → `2027`; `'99999999999999999.99' + 0.01` → `1.0e+17`).
//
// The column is dropped from eligibility rather than the config rejected,
// because the *only* thing wrong is the arithmetic: the struct field,
// scanning, filters and sorts are all correct and wanted. That matters most on
// SQLite, where TEXT is the only affinity that stores an exact decimal —
// NUMERIC converts `'12.34'` to REAL on insert — so `text` + `decimal.Decimal`
// is the correct design for money there, and rejecting it would leave no way
// to express exact decimals at all. The warning is what keeps the drop from
// being silent, which is the whole objection to dropping it.
//
// Deliberately as coarse as the §29.2.4 tenancy guard it mirrors: a SQL type
// the classifier cannot place is trusted rather than guessed at. It reads the
// resolved (BaseSQLType, GoType) pair rather than the config, so it needs no
// notion of which override route was taken — and a column nobody overrode
// cannot reach it, because no non-arithmetic SQL type resolves to an
// arithmetic Go type on its own.
func incrementSQLTypeWarnings(qualifiedTable string, tableCfg config.TableConfig, columns []ColumnContext) []string {
	var warnings []string
	for _, col := range columns {
		if !incrementBlockedBySQLType(col) {
			continue
		}
		sqlTypeDesc := fmt.Sprintf("%q", col.SQLType)
		if !strings.EqualFold(col.BaseSQLType, col.SQLType) {
			sqlTypeDesc = fmt.Sprintf("%q (a domain over %q)", col.SQLType, col.BaseSQLType)
		}
		warnings = append(warnings, fmt.Sprintf(
			"%s: %q is an arithmetic Go type but column %q has SQL type %s, so %s is not offered for increment — `%s = %s + ?` would error on PostgreSQL, coerce on MySQL, and silently rewrite the stored value on SQLite. Everything else about the override applies as written.",
			typeOverrideSource(qualifiedTable, tableCfg, col, col.BaseSQLType), col.GoType, col.Name, sqlTypeDesc,
			qualifiedTable+"."+col.Name, col.Name, col.Name,
		))
	}
	return warnings
}

// typeOverrideSource names the config key that produced a column's resolved Go
// type, so an error can point at the line to edit. The two per-column routes
// are named exactly. For the by-SQL-type routes, the table-level map is probed
// first so the error carries the `tables.<t>.` prefix when that is where the
// entry lives, and the key is reported as written — the raw SQL type when the
// user keyed on it, the domain-resolved base otherwise — rather than as a
// spelling (`varchar(255)`) that appears in no config file.
func typeOverrideSource(qualifiedTable string, tableCfg config.TableConfig, col ColumnContext, baseSQLType string) string {
	if override, ok := tableCfg.ColumnMap[col.Name]; ok && override.Type != "" {
		return fmt.Sprintf("tables.%s.column_map.%s.type", qualifiedTable, col.Name)
	}
	if _, ok := tableCfg.TypeMap[col.Name]; ok {
		return fmt.Sprintf("tables.%s.type_map.%s", qualifiedTable, col.Name)
	}
	if tableCfg.Overrides != nil {
		for _, key := range []string{col.SQLType, baseSQLType} {
			if _, ok := tableCfg.Overrides.Types[key]; ok {
				return fmt.Sprintf("tables.%s.overrides.types.%s", qualifiedTable, key)
			}
		}
	}
	return fmt.Sprintf("overrides.types.%s", baseSQLType)
}

// needsExplicitImport reports whether a Go type literal names a package
// gotype cannot supply on its own. Same-package types (`Address`) and
// builtins carry no qualifier; `time.Time`, `json.RawMessage` and the rest of
// the known registry carry one but resolve their own import.
func needsExplicitImport(literal string) bool {
	if !strings.Contains(literal, ".") {
		return false
	}
	_, known := gotype.KnownLiteral(literal)
	return !known
}

// buildUpdateColumnContexts pairs each auto-set update column with its
// resolved Go field name. The update template needs both spellings and must
// not re-derive the second from the first.
func buildUpdateColumnContexts(updateColumns []string, columns []ColumnContext) []UpdateColumnContext {
	if len(updateColumns) == 0 {
		return nil
	}
	out := make([]UpdateColumnContext, 0, len(updateColumns))
	for _, name := range updateColumns {
		out = append(out, UpdateColumnContext{Name: name, FieldName: columnFieldName(columns, name)})
	}
	return out
}

// qualifiedTableOrName returns the schema-qualified table name, or the bare
// name if the schema is empty.
func qualifiedTableOrName(t *parser.Table) string {
	if t.Schema == "" {
		return t.Name
	}
	return t.Schema + "." + t.Name
}

// tableHasResolvedPK reports whether a table has a primary key after override
// resolution. A table qualifies when either:
//   - The matching config.TableConfig declares primary_key.columns (override), or
//   - At least one of the parser-detected columns has PrimaryKey == true.
//
// Tables that fail both checks are skipped from generation; the warning is
// emitted earlier from config.ValidatePostParse::validateMissingPrimaryKey
// (PRD §9.4b).
func tableHasResolvedPK(cfg *config.RootConfig, table *parser.Table) bool {
	if cfg != nil {
		if tc, ok := cfg.Tables[qualifiedTableOrName(table)]; ok {
			if tc.PrimaryKey != nil && len(tc.PrimaryKey.Columns) > 0 {
				return true
			}
		} else if tc, ok := cfg.Tables[table.Name]; ok {
			if tc.PrimaryKey != nil && len(tc.PrimaryKey.Columns) > 0 {
				return true
			}
		}
	}
	for i := range table.Columns {
		if table.Columns[i].PrimaryKey {
			return true
		}
	}
	return false
}

// columnContextNames returns the slice of column names from a ColumnContext
// slice, preserving order.
func columnContextNames(cols []ColumnContext) []string {
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name
	}
	return names
}

// hasBareSliceColumn reports whether any column is a bare Go slice (e.g., []string)
// that requires pq.Array wrapping for stdlib scanning. Named slice types like
// UserRoleSlice have their own Scan/Value and do not need pq.Array.
func hasBareSliceColumn(columns []ColumnContext) bool {
	for _, col := range columns {
		if col.IsSlice && col.SliceElemType == "" {
			return true
		}
	}
	return false
}

// collectColumnImports returns a deduplicated, sorted list of import paths
// from all columns. This is used by templates to emit explicit imports.
func collectColumnImports(columns []ColumnContext) []string {
	return UniqueImports(func() []string {
		var imports []string
		for _, col := range columns {
			if col.Import != "" {
				imports = append(imports, col.Import)
			}
		}
		return imports
	}())
}

// --- Column building ---

func buildColumnContext(input *GenerateInput, col *parser.Column, tableCfg config.TableConfig, tableOverrides map[string]config.TypeOverride) ColumnContext {
	gt := resolveColumnGoType(input.Resolver, col, tableCfg, tableOverrides, input.Config.Tenancy)

	accessRole, accessCaps := resolveAccess(tableCfg.ColumnMap[col.Name].Access)
	fieldName, fieldNameOverridden := resolveColumnFieldName(col.Name, tableCfg)

	var fkRef *FKReferenceContext
	if col.FKReference != nil {
		fkRef = &FKReferenceContext{
			Table:     col.FKReference.Table,
			Schema:    col.FKReference.Schema,
			Column:    col.FKReference.Column,
			Synthetic: col.FKReference.Synthetic,
		}
	}

	return ColumnContext{
		Name:          col.Name,
		FieldName:     fieldName,
		GoType:        gt.Name,
		Import:        gt.Import,
		ZeroValue:     gt.ZeroValue,
		SQLType:       col.Type,
		BaseSQLType:   baseSQLType(input.Resolver, col.Type),
		Nullable:      col.Nullable,
		PrimaryKey:    col.PrimaryKey,
		HasDefault:    col.Default != "",
		DefaultExpr:   resolveDefaultExpr(col),
		AutoIncrement: col.AutoIncrement,
		IsComputed:    col.GeneratedExpr != "",
		Description:   resolveColumnDescription(col, tableCfg),
		DBTag:         col.Name,
		JSONTag:       col.Name,
		IsSlice:       gt.IsSlice,
		SliceElemType: gt.SliceElemType,
		IsSet:         input.Resolver.IsSet(col.Type),
		FKConvert:     gt.FKConvert,
		FKReference:   fkRef,
		Unique:        col.Unique,

		FieldNameOverridden: fieldNameOverridden,

		Access:             accessRole,
		APIReadable:        accessCaps.APIReadable,
		APIWritable:        accessCaps.APIWritable,
		APIFilterable:      accessCaps.APIFilterable,
		APISortable:        accessCaps.APISortable,
		EventRedacted:      accessCaps.EventRedacted,
		ManifestVisibility: accessCaps.ManifestVisibility,
	}
}

// --- Doc comment resolution ---

// resolveTableDescription resolves the doc comment for a table.
// Priority: config description > SQL COMMENT ON TABLE > empty.
// All descriptions are returned verbatim — the template handles formatting.
func resolveTableDescription(table *parser.Table, tableCfg config.TableConfig) string {
	if tableCfg.Description != "" {
		return tableCfg.Description
	}
	return table.Comment
}

// resolveColumnFieldName resolves a column's Go field name: the
// `column_map.<col>.name` override when set, otherwise the naming engine's
// PascalCase form (PRD §8.5 "Field Name Resolution"). The second return
// reports whether the override supplied the name.
//
// This is the single point at which a column's Go identifier is decided;
// every other site reads ColumnContext.FieldName rather than re-deriving it
// from the SQL name, so the override reaches the entity struct, the filter,
// the create/update inputs, the field options, the cache, the manifest, the
// event payloads and the gqlgen handoff without further plumbing.
//
// Validation (a legal exported identifier, an existing non-excluded column)
// runs in config.ValidatePreParse / ValidatePostParse; the resolved-name
// collision rule runs in validateResolvedFieldNames, which needs the names
// this function produces.
func resolveColumnFieldName(column string, tableCfg config.TableConfig) (string, bool) {
	if override, ok := tableCfg.ColumnMap[column]; ok && override.Name != "" {
		return override.Name, true
	}
	return FieldName(column), false
}

// resolveColumnGoType resolves a column's Go type: the tenant column's
// `tenancy.type` override first, then the `column_map.<col>.type` override,
// otherwise the Resolver's six-step chain (PRD §8.5 "Column Overrides";
// `column_map` beats `type_map` per PRD §4.7).
//
// `tenancy.type` sits above `column_map` because §29.2.4 orders the tenant
// column's resolution that way — per-table `tenancy.type`, then global
// `tenancy.type`, then the column's own chain with `column_map` at its head.
// Resolving it here rather than only in the tenancy context is what stops the
// row struct and the tenancy context from spelling one column two ways; see
// tableTenantTypeOverride for what that divergence cost.
//
// The override is applied here rather than inside Resolver.Resolve because
// `column_map`-beats-`type_map` is a config-precedence decision, not a
// type-resolution one — the resolver's chain and signature stay untouched.
//
// `.import` wins when set; otherwise the import is whatever gotype already
// knows for the literal, so `column_map.<col>.type: time.Time` needs no
// explicit import. A literal that is package-qualified, unknown and
// import-less is rejected by validateColumnTypeLiterals before we get here.
//
// Nullability follows FromLiteral — `*T`, or bare `T` for natively-nilable
// types — exactly as `type_map` behaves. `column_map` has no `nullable:`
// sub-field, so it does not reach the Null-wrapper machinery
// `overrides.types` offers via TypeOverride.Nullable (PRD §8.5).
func resolveColumnGoType(resolver *gotype.Resolver, col *parser.Column, tableCfg config.TableConfig, tableOverrides map[string]config.TypeOverride, tenancy *config.TenancyConfig) gotype.GoType {
	if gt, ok := tableTenantTypeOverride(col, tableCfg, tenancy); ok {
		return gt
	}
	if override, ok := tableCfg.ColumnMap[col.Name]; ok && override.Type != "" {
		gt := gotype.FromLiteral(override.Type, col.Nullable)
		if override.Import != "" {
			gt.Import = override.Import
		}
		return gt
	}
	return resolver.Resolve(col.Type, col.Nullable, col.Name, tableCfg.TypeMap, tableOverrides)
}

// columnFieldName returns the resolved Go field name for the named column,
// falling back to the naming engine when the column is absent from cols
// (excluded, or a hand-built unit fixture that omits it).
func columnFieldName(cols []ColumnContext, column string) string {
	for i := range cols {
		if cols[i].Name == column {
			return cols[i].FieldName
		}
	}
	return FieldName(column)
}

// markTenantColumn sets ColumnContext.Tenant on the named column. Callers pass
// each []ColumnContext on the entity separately — Columns and PKColumns hold
// independent copies of the same column, so stamping one leaves the other
// saying the opposite. A column absent from cols (excluded via
// `exclude_columns`, or a hand-built fixture that omits it) is a no-op.
func markTenantColumn(cols []ColumnContext, column string) {
	for i := range cols {
		if cols[i].Name == column {
			cols[i].Tenant = true
		}
	}
}

// resolveColumnDescription resolves the doc comment for a column.
// Priority: config column_map.<col>.description > SQL COMMENT ON COLUMN > empty.
// All descriptions are returned verbatim — the template handles formatting.
func resolveColumnDescription(col *parser.Column, tableCfg config.TableConfig) string {
	if tableCfg.ColumnMap != nil {
		if override, ok := tableCfg.ColumnMap[col.Name]; ok && override.Description != "" {
			return override.Description
		}
	}
	return col.Comment
}

// resolveDefaultExpr returns the raw SQL expression to use in place of the
// DEFAULT keyword for dialects that do not support it (SQLite). For columns
// with an explicit DEFAULT, this is the expression itself. For nullable columns
// without a DEFAULT, this is "NULL". Returns empty string when neither applies.
func resolveDefaultExpr(col *parser.Column) string {
	if col.Default != "" {
		return col.Default
	}
	if col.Nullable {
		return "NULL"
	}
	return ""
}

// --- PK strategy detection ---

// detectPKStrategy determines the PK strategy for a table following PRD 8.6.
func detectPKStrategy(input *GenerateInput, _ *parser.Table, pkColumns []ColumnContext, tableCfg config.TableConfig) config.PKStrategy {
	// Step 1: explicit table-level override.
	if tableCfg.PrimaryKey != nil && tableCfg.PrimaryKey.Strategy != "" {
		switch tableCfg.PrimaryKey.Strategy {
		case config.PKStrategyDB:
			return config.PKStrategyDB
		case config.PKStrategyApp:
			return config.PKStrategyApp
		case config.PKStrategyCaller:
			return config.PKStrategyCaller
		}
	}

	if len(pkColumns) == 0 || len(pkColumns) > 1 {
		// Composite PKs always require caller-provided values (foreign keys, not auto-generated).
		return config.PKStrategyCaller
	}

	// Step 2: auto-detection from schema + dialect (single-column PK only).
	return autoDetectPKStrategy(input.Config.Input.Dialect, pkColumns[0])
}

func autoDetectPKStrategy(dialect config.Dialect, pk ColumnContext) config.PKStrategy {
	// A single-column PK that is also a foreign key is caller-supplied by
	// definition (PRD §8.6): its value must equal an existing parent row's
	// key, so neither the database nor the generated client may mint it.
	// Without this clause the dialect arms below classify such a PK on type
	// alone — a UUID 1:1 extension key resolved to `app`, and Create then
	// minted a fresh UUID that could not satisfy the foreign key.
	//
	// Synthetic references are excluded: they are invented from a config
	// `relationships.fk` and can land on a column that holds no foreign key,
	// including a table's own surrogate PK, which would demote a healthy
	// `db` key to `caller`.
	if pk.FKReference != nil && !pk.FKReference.Synthetic {
		return config.PKStrategyCaller
	}
	switch dialect {
	case config.DialectPostgres:
		return detectPostgresPK(pk)
	case config.DialectMySQL:
		return detectMySQLPK(pk)
	case config.DialectSQLite:
		return detectSQLitePK(pk)
	}
	return config.PKStrategyCaller
}

func detectPostgresPK(pk ColumnContext) config.PKStrategy {
	sqlType := strings.ToLower(pk.SQLType)
	switch {
	case isUUIDType(sqlType) && pk.HasDefault:
		return config.PKStrategyDB
	case isUUIDType(sqlType):
		return config.PKStrategyApp
	case isSerialType(sqlType), pk.AutoIncrement:
		return config.PKStrategyDB
	case pk.HasDefault:
		return config.PKStrategyDB
	default:
		return config.PKStrategyCaller
	}
}

func detectMySQLPK(pk ColumnContext) config.PKStrategy {
	sqlType := strings.ToLower(pk.SQLType)
	switch {
	case pk.AutoIncrement:
		return config.PKStrategyDB
	case isUUIDType(sqlType):
		// MySQL + UUID always defaults to app — no way to retrieve DB-generated UUID.
		return config.PKStrategyApp
	default:
		return config.PKStrategyCaller
	}
}

func detectSQLitePK(pk ColumnContext) config.PKStrategy {
	sqlType := strings.ToLower(pk.SQLType)
	switch {
	case pk.AutoIncrement || (isIntegerType(sqlType) && pk.HasDefault):
		return config.PKStrategyDB
	case isUUIDType(sqlType) && pk.HasDefault:
		return config.PKStrategyDB
	case isUUIDType(sqlType):
		return config.PKStrategyApp
	default:
		return config.PKStrategyCaller
	}
}

func isUUIDType(sqlType string) bool {
	return sqlType == "uuid"
}

func isSerialType(sqlType string) bool {
	return sqlType == "serial" || sqlType == "bigserial" || sqlType == "smallserial"
}

func isIntegerType(sqlType string) bool {
	switch sqlType {
	case "integer", "int", "bigint", "smallint", "tinyint", "int2", "int4", "int8":
		return true
	}
	return false
}

// resolveUUIDVersion resolves the UUID version for app-generated PKs.
func resolveUUIDVersion(tableCfg config.TableConfig, cfg *config.RootConfig) config.UUIDVersion {
	if tableCfg.PrimaryKey != nil && tableCfg.PrimaryKey.UUIDVersion != "" {
		return tableCfg.PrimaryKey.UUIDVersion
	}
	if cfg.Generation.UUIDVersion != "" {
		return cfg.Generation.UUIDVersion
	}
	return config.UUIDVersionV4
}

// --- Soft delete helpers ---

func softDeleteColumnNames(configs []config.SoftDeleteConfig) []string {
	names := make([]string, len(configs))
	for i, c := range configs {
		names[i] = c.Name
	}
	return names
}

// --- ExcludeDeleted resolution ---

// resolveExcludeDeleted determines whether methods should inject default
// "is not deleted" conditions. nil config means auto-detect from soft delete presence.
func resolveExcludeDeleted(configVal *bool, softDelete *SoftDeleteContext) bool {
	if configVal != nil {
		return *configVal
	}
	return softDelete != nil
}

// --- Operations resolution ---

// toResolvedOperations is the table's client method set before the two gates
// applied after it (the Connection cursor-key decision and the conflict
// target). The client has no operations toggle (PRD §4.6): every method the
// schema allows is generated, so each field here is a schema fact or true.
//
// The three nested methods start false. wireNestedMutations sets each to
// whether the method was emitted, once every table context exists and edge
// eligibility (§9.9.4) can be decided; `nested_mutations` alone governs them.
func toResolvedOperations(softDelete *SoftDeleteContext, columns []ColumnContext) ResolvedOperations {
	return ResolvedOperations{
		Get:         true,
		GetMany:     true,
		Count:       true,
		Create:      true,
		CreateMany:  true,
		Update:      true,
		UpdateMany:  true,
		UpdateWhere: true,
		Upsert:      true,
		UpsertMany:  true,
		HardDelete:  true,
		Exists:      true,
		Paginate:    true,
		Connection:  true,
		Stream:      true,
		// Soft delete and restore need a detected soft-delete column (§5.4);
		// Increment needs a numeric column that identifies nothing (§8.2).
		SoftDelete: softDelete != nil,
		Restore:    softDelete != nil,
		Increment:  len(buildIncrementColumns(columns)) > 0,
	}
}

// omitUpsertWithoutConflictTarget turns the upsert family off on a table that
// emits no conflict-target constant.
//
// That is an app-enforced key with no UNIQUE beside it (PRD §9.5). The only
// value the `target` argument could then take is the zero value, which names no
// conflict columns, so the builder drops the conflict clause and the call is a
// plain INSERT: a second Upsert of the same key inserts a duplicate row and
// returns the first one, and UpsertMany returns more entities than it was
// given. The methods are omitted rather than emitted uncallable — the
// conflict-target rule for the nested surface, applied to the methods it
// composes — and every consumer of the resolved mask (client interface,
// manifest, events, API) follows from this one place. UpsertWithRelated needs
// no clearing here: it starts false, and wireNestedMutations applies the same
// conflict-target rule when it decides EmitUpsert.
func omitUpsertWithoutConflictTarget(ops *ResolvedOperations, targets []ConflictTargetContext) {
	if len(targets) > 0 {
		return
	}
	ops.Upsert = false
	ops.UpsertMany = false
}

// deref returns the value pointed to by p, or the zero value of T if p is nil.
func deref[T any](p *T) T {
	if p != nil {
		return *p
	}
	var zero T
	return zero
}

// --- Relationship building ---

// buildRelationshipContexts assembles a table's relationship set from the two
// sources that feed it — the FK-inferred edges parser.DetectRelationships hung
// on schema.Relationships, and the explicit `tables.<t>.relationships` entries
// — with `exclude_relationships` applied to both. It returns the sorted set
// plus any non-fatal notices raised while merging the two sources.
//
// A config-declared relationship that resolves to the same Go field as an
// inferred one **replaces** it (PRD §4.8, §13.4). Appending both edges would
// put the field on the entity struct twice and the models package would not
// compile — `sqlgen validate` and `sqlgen generate` would both report success
// and `go build` would fail. The match
// runs on FieldName rather than Name because that is the identifier that
// actually collides: a declared name is PascalCase as written (`Entries`) and
// an inferred one is the pluralized snake form (`entries`), and both resolve
// through relationshipFieldAndType onto the same `Entries` field.
//
// The replaced edge is reported as a warning rather than dropped silently.
// The config is valid and the result is what was asked for, so this is not an
// error — but an inferred edge vanishing with no trace is the same objection
// that made incrementSQLTypeWarnings a warning. `exclude_relationships` stays
// the way to drop an inferred edge without declaring a replacement, and
// remains required when the replacement is named something else.
//
// Every edge's field type is its target's generated struct name, read off
// targets. A target that generates nothing splits the two sources
// the way PRD §4.13 splits listed and auto-included entries: an inferred edge
// into it is omitted, and a declared one is an error (checkDeclaredTarget).
func buildRelationshipContexts(
	schema *parser.Schema, table *parser.Table, tableCfg config.TableConfig, targets *relationshipTargets,
) ([]RelationshipContext, []string, error) {
	excludeSet := make(map[string]bool)
	for _, name := range tableCfg.ExcludeRelationships {
		excludeSet[name] = true
	}

	var inferred []RelationshipContext

	qualifiedName := table.Name
	if table.Schema != "" {
		qualifiedName = table.Schema + "." + table.Name
	}

	for _, r := range schema.Relationships {
		if r.SourceTable != qualifiedName {
			continue
		}
		// Bare-table exclude entries (e.g. `exclude_relationships:
		// [asset]`) match every disambiguated edge in a multi-FK group via
		// BaseName, while a user can also target one specific edge by its
		// post-rewrite Name (e.g. `[developer_assets]`). Slice-shaped
		// relationships are pluralized on the codegen side to match the
		// generated field name (`DeveloperAssets []*Asset`), so the
		// disambiguated-form match runs against the pluralized name —
		// otherwise users excluding by the form they see in generated code
		// would silently fail to match.
		visibleName := r.Name
		if r.Type == parser.OneToMany || r.Type == parser.ManyToMany {
			visibleName = toPlural(r.Name)
		}
		if excludeSet[visibleName] || excludeSet[r.BaseName] {
			continue
		}
		// An FK into a table that generates nothing — excluded, or with no
		// primary key (PRD §6.4, §9.4b) — has no type to name. The edge was
		// never asked for, so it is omitted rather than failing a build that
		// the skipped table is otherwise not part of (§4.13's auto-included
		// rule).
		if !targets.generates(r.TargetTable) {
			continue
		}
		inferred = append(inferred, relationshipToContext(schema, r, targets))
	}

	rels, warnings, err := mergeDeclaredRelationships(schema, inferred, tableCfg, excludeSet, targets, qualifiedName)
	if err != nil {
		return nil, nil, err
	}
	slices.SortFunc(rels, func(a, b RelationshipContext) int {
		return strings.Compare(a.Name, b.Name)
	})
	return rels, warnings, nil
}

// mergeDeclaredRelationships folds `tables.<t>.relationships` into the
// FK-inferred set, applying the §4.8 / §13.4 replacement rule: a declared
// entry that resolves to the same Go field as an inferred one supersedes it.
// It returns the merged set in source order (caller sorts) plus one notice per
// replacement.
func mergeDeclaredRelationships(
	schema *parser.Schema, inferred []RelationshipContext, tableCfg config.TableConfig,
	excludeSet map[string]bool, targets *relationshipTargets, qualifiedName string,
) ([]RelationshipContext, []string, error) {
	// Index the inferred edges by resolved field name so a declared entry can
	// find the one it replaces. disambiguateRelationshipNames groups by
	// (source, target), so no multi-FK group can claim one field twice;
	// last-write-wins on this index is reachable only from two edges to
	// *different* targets whose names pluralize onto one field, which
	// validateDuplicateRelationshipFields rejects outright.
	inferredByField := make(map[string]int, len(inferred))
	for i := range inferred {
		inferredByField[inferred[i].FieldName] = i
	}

	var (
		declared []RelationshipContext
		warnings []string
	)
	replaced := make(map[int]bool)
	// Every unresolvable target is reported, not only the first, so one run of
	// `sqlgen validate` lists them all.
	var errs []error
	seenKey := make(map[string]int)
	for i, r := range tableCfg.Relationships {
		if err := checkResolvedDuplicate(r, i, tableCfg.Relationships, targets, seenKey, qualifiedName); err != nil {
			errs = append(errs, err)
		}
		if excludeSet[r.Name] {
			continue
		}
		// Both checks run so one pass reports an ambiguous `table:` and an
		// ambiguous `junction:` on the same entry together.
		targetErr := checkDeclaredTarget(r, targets, qualifiedName)
		junctionErr := checkDeclaredJunction(r, targets, qualifiedName)
		if targetErr != nil || junctionErr != nil {
			errs = append(errs, errors.Join(targetErr, junctionErr))
			continue
		}
		rel := configRelationshipToContext(schema, r, targets)
		// The !replaced guard keeps a second declared entry resolving to the
		// same field from re-reporting a replacement that already happened.
		// It does not make that pair legal: config.validateRelationships
		// compares `name:` byte-equal, so `primary_document` and
		// `PrimaryDocument` are distinct names claiming one field and both
		// pass it. validateDuplicateRelationshipFields rejects the merged
		// set (§13.7.3 row 2).
		if i, ok := inferredByField[rel.FieldName]; ok && !replaced[i] {
			replaced[i] = true
			warnings = append(warnings, fmt.Sprintf(
				"tables.%s.relationships: %q replaces the foreign-key-inferred relationship %q — both resolve to the Go field %q (PRD §13.4). Rename the declared relationship to keep both edges.",
				qualifiedName, rel.Name, inferred[i].Name, rel.FieldName,
			))
		}
		declared = append(declared, rel)
	}

	rels := make([]RelationshipContext, 0, len(inferred)+len(declared))
	for i := range inferred {
		if replaced[i] {
			continue
		}
		rels = append(rels, inferred[i])
	}
	if err := errors.Join(errs...); err != nil {
		return nil, nil, err
	}
	return append(rels, declared...), warnings, nil
}

func relationshipToContext(schema *parser.Schema, r parser.Relationship, targets *relationshipTargets) RelationshipContext {
	tSchema, tName := splitQualifiedName(r.TargetTable)
	// Callers drop an edge whose target does not generate before building it.
	target, _ := targets.resolve(tSchema, tName)
	targetStruct := target.StructName

	// Slice-shaped relationships (O2M / M2M) emit plural identifiers
	// regardless of the source table's casing — toPlural returns an
	// already-plural word unchanged, so schemas with plural table
	// names (`reviews`, `tags`) stay byte-stable while singular schemas
	// (`review`, `tag`) and disambiguated edges (`developer_asset` →
	// `developer_assets`) get the expected `Reviews []*Review`,
	// `Tags []*Tag`, `DeveloperAssets []*Asset` field shape rather than a
	// singular field bearing a slice type. Pluralize once at the snake
	// name so FieldName, JSONTag, and the API-side SQLName/GraphQLName
	// (derived from `Name` in mapRelationshipToGraphQL) all stay aligned.
	// `exclude_relationships` matching is unaffected — it runs against the
	// parser-side `Relationship.Name` / `BaseName` before this rewrite.
	//
	// That pass-through is a property of the English rules, not a guarantee:
	// a name whose last word is a canonical acronym or a non-plural "-s" noun
	// is frozen and always takes an appended marker instead, so `user_ips`
	// pluralizes to `user_ipses` (PRD §8.5).
	name := r.Name
	if r.Type == parser.OneToMany || r.Type == parser.ManyToMany {
		name = toPlural(name)
	}
	fieldName, goType := relationshipFieldAndType(name, r.Type, targetStruct)

	sorts := make([]SortContext, 0, len(r.Sort))
	for _, s := range r.Sort {
		sorts = append(sorts, SortContext{Column: s.Column, Direction: sortDirection(s.Direction)})
	}

	jSchema, jName := splitQualifiedName(r.JunctionTable)
	return RelationshipContext{
		Name:                name,
		Type:                r.Type,
		Side:                r.Side,
		TargetTable:         tName,
		TargetSchema:        tSchema,
		TargetStructName:    targetStruct,
		FKColumn:            r.FKColumn,
		FKOnTarget:          fkColumnOnTarget(schema, r.Type, tSchema, tName, r.FKColumn),
		JunctionTable:       jName,
		JunctionSchema:      jSchema,
		JunctionLocalFK:     r.JunctionLocalFK,
		JunctionReferenceFK: r.JunctionReferenceFK,
		Filter:              r.Filter,
		Sort:                sorts,
		FieldName:           fieldName,
		GoType:              goType,
		Description:         relationshipDescription(r.Type, tName),
		JSONTag:             toSnakeName(name),
	}
}

// sortDirection normalizes a relationship `sort:` direction to the sql package's
// spelling ("ASC" / "DESC"). Config validation admits only asc / desc in either
// case, and empty means ascending (PRD §4.8).
func sortDirection(d string) string {
	if strings.EqualFold(d, "desc") {
		return "DESC"
	}
	return "ASC"
}

func configRelationshipToContext(schema *parser.Schema, r config.TableRelationship, targets *relationshipTargets) RelationshipContext {
	relType := parseRelationshipType(r.Type)
	// Callers run checkDeclaredTarget first, so the target resolves. The
	// context carries the schema the target resolved in, not the one `table:`
	// spelled: a bare name resolves only when one entity has it, and carrying
	// that entity's schema makes every (TargetSchema, TargetTable) lookup
	// downstream exact.
	_, tName := splitQualifiedName(r.Table)
	target, _ := targets.resolve(splitQualifiedName(r.Table))
	fieldName, goType := relationshipFieldAndType(r.Name, relType, target.StructName)

	sorts := make([]SortContext, 0, len(r.Sort))
	for _, s := range r.Sort {
		sorts = append(sorts, SortContext{Column: s.Column, Direction: sortDirection(s.Direction)})
	}

	// The junction carries its resolved schema for the same reason, so the
	// loader, the relationship filter, the FK metadata and the nested link
	// step name one table. Callers run checkDeclaredJunction first.
	cjSchema, cjName, _ := targets.resolveJunction(r.Junction)
	return RelationshipContext{
		Name:                r.Name,
		Type:                relType,
		Side:                parseRelationshipSide(r.Side),
		TargetTable:         tName,
		TargetSchema:        target.Schema,
		TargetStructName:    target.StructName,
		FKColumn:            r.FK,
		FKOnTarget:          fkColumnOnTarget(schema, relType, target.Schema, tName, r.FK),
		JunctionTable:       cjName,
		JunctionSchema:      cjSchema,
		JunctionLocalFK:     r.JunctionLocalFK,
		JunctionReferenceFK: r.JunctionReferenceFK,
		Filter:              r.Filter,
		Discriminator:       discriminatorContext(r.Discriminator),
		Sort:                sorts,
		FieldName:           fieldName,
		GoType:              goType,
		Description:         relationshipDescription(relType, r.Table),
		JSONTag:             toSnakeName(r.Name),
	}
}

// fkColumnOnTarget derives RelationshipContext.FKOnTarget: whether the edge's
// FK column is declared on the target table rather than on the source. This is
// the one place the fact is computed — the O2O join builder used to re-derive
// it inline by scanning the same columns, and the write-side emitters need the
// same answer (PRD §13.1).
//
// The lookup is (targetSchema, targetTable) exactly as the relationship context
// spells them, so it resolves against the same table as the rest of the
// context. Both builders pass the target's own schema: the parser qualifies an
// FK-inferred target, and configRelationshipToContext carries the schema the
// declared `table:` resolved in. findSchemaTable's first-match
// fallback is therefore reached only in a dialect without schemas.
//
// false is therefore three answers in one: the FK is on the source, the target
// is unresolved, or the edge is M2M. A write-side consumer that needs to tell
// them apart must check the target separately rather than read false as
// "belongs-to".
//
// A self-referential O2O resolves true, because source and target are one table
// and the FK is on it either way. That is the answer the read path has always
// produced for this shape; the two sides of the ON clause are the same table's
// FK and PK regardless of which way round they are read.
func fkColumnOnTarget(schema *parser.Schema, relType parser.RelationshipType, targetSchema, targetTable, fkColumn string) bool {
	if schema == nil || fkColumn == "" {
		return false
	}
	// M2M carries both FKs on the junction table, so neither end of the edge
	// holds one: auto-detected M2M edges leave FKColumn empty and a config edge
	// spells its junction FKs separately.
	if relType != parser.OneToOne && relType != parser.OneToMany {
		return false
	}
	target := findSchemaTable(schema, targetSchema, targetTable)
	if target == nil {
		return false
	}
	return slices.ContainsFunc(target.Columns, func(c parser.Column) bool {
		return c.Name == fkColumn
	})
}

// relationshipDescription returns a doc comment description for a relationship field.
func relationshipDescription(relType parser.RelationshipType, targetTable string) string {
	var kind string
	switch relType {
	case parser.OneToOne:
		kind = "one-to-one"
	case parser.OneToMany:
		kind = "one-to-many"
	case parser.ManyToMany:
		kind = "many-to-many"
	default:
		kind = "unknown"
	}
	return kind + " relationship with the " + targetTable + " table."
}

func relationshipFieldAndType(name string, relType parser.RelationshipType, targetStruct string) (string, string) {
	fieldName := toPascalCase(name)
	var goType string
	switch relType {
	case parser.OneToOne:
		goType = "*" + targetStruct
	case parser.OneToMany, parser.ManyToMany:
		goType = "[]*" + targetStruct
	}
	return fieldName, goType
}

// parseRelationshipType maps the YAML `type:` field on a TableRelationship to
// the parser-side enum. Config validation (isValidRelationshipType in
// cmd/sqlgen/config/validate.go) admits exactly these six spellings and
// rejects an empty one, so the trailing one-to-many default is not reached
// for a validated config.
func parseRelationshipType(t string) parser.RelationshipType {
	switch strings.ToLower(t) {
	case "one_to_one", "o2o":
		return parser.OneToOne
	case "one_to_many", "o2m":
		return parser.OneToMany
	case "many_to_many", "m2m":
		return parser.ManyToMany
	}
	return parser.OneToMany
}

// parseRelationshipSide maps the YAML `side:` field on a TableRelationship to
// the parser-side enum. Empty / unknown values resolve to SideParent — every
// auto-detected edge is parent-side today, and config-defined relationships
// almost always describe the FK-holder. Inverse-side config entries opt in
// explicitly via `side: child`.
func parseRelationshipSide(s string) parser.RelationshipSide {
	switch strings.ToLower(s) {
	case "child":
		return parser.SideChild
	case "", "parent":
		return parser.SideParent
	}
	return parser.SideParent
}

// --- Conflict targets ---

func buildConflictTargets(table *parser.Table, tableCfg config.TableConfig, structName string) []ConflictTargetContext {
	var targets []ConflictTargetContext

	var pkCols []string
	for _, col := range table.Columns {
		if col.PrimaryKey {
			pkCols = append(pkCols, col.Name)
		}
	}
	if len(pkCols) > 0 && pkConflictTargetIsIndexed(table, tableCfg, pkCols) {
		targets = append(targets, ConflictTargetContext{
			ConstantName: structName + "ConflictPK",
			Columns:      pkCols,
			Comment:      "PRIMARY KEY (" + strings.Join(pkCols, ", ") + ")",
		})
	}

	// Track columns already covered by table-level UNIQUE constraints.
	// Partial UNIQUE indexes (c.Where != "") are skipped: ON CONFLICT against
	// a partial index requires the predicate, which the generated upsert
	// surface does not model — emitting a bare conflict target would silently
	// produce wrong runtime behavior for rows outside the predicate.
	coveredCols := make(map[string]bool)
	for _, c := range table.Constraints {
		if c.Type != parser.Unique || c.Where != "" {
			continue
		}
		for _, col := range c.Columns {
			coveredCols[col] = true
		}
		constName := structName + "Conflict" + conflictConstantSuffix(c.Columns)
		targets = append(targets, ConflictTargetContext{
			ConstantName: constName,
			Columns:      c.Columns,
			Comment:      "UNIQUE (" + strings.Join(c.Columns, ", ") + ")",
		})
	}

	// Also pick up inline UNIQUE columns not covered by table-level constraints.
	for _, col := range table.Columns {
		if !col.Unique || col.PrimaryKey || coveredCols[col.Name] {
			continue
		}
		constName := structName + "Conflict" + conflictConstantSuffix([]string{col.Name})
		targets = append(targets, ConflictTargetContext{
			ConstantName: constName,
			Columns:      []string{col.Name},
			Comment:      "UNIQUE (" + col.Name + ")",
		})
	}

	// A target's columns decide whether a batched upsert can take the written
	// row's key from the input, so the answer is resolved once here rather
	// than re-derived per template (PRD §9.5, §9.7).
	for i := range targets {
		targets[i].CoversPK = len(pkCols) > 0 &&
			!slices.ContainsFunc(pkCols, func(pk string) bool {
				return !slices.Contains(targets[i].Columns, pk)
			})
	}

	slices.SortFunc(targets, func(a, b ConflictTargetContext) int {
		return strings.Compare(a.ConstantName, b.ConstantName)
	})
	return targets
}

// pkConflictTargetIsIndexed reports whether an ON CONFLICT clause naming the
// resolved primary-key columns will find an index at runtime.
//
// A schema-declared key always will: the database built the index when it
// accepted the PRIMARY KEY. A key declared by tables.<name>.primary_key.columns
// is only as good as whatever UNIQUE the schema actually carries — PRD §8.6
// lets the config assert uniqueness the database does not enforce ("sqlgen
// assumes uniqueness"), which validatePrimaryKeyOverride reports as a warning
// rather than an error. Emitting the target anyway would generate an Upsert
// arm that compiles and then fails on every call: PostgreSQL rejects it with
// 42P10, "there is no unique or exclusion constraint matching the ON CONFLICT
// specification". So the override case has to earn its target.
//
// This reads the override off the config rather than off the column flags
// because, once cli.applyPrimaryKeyOverrides has resolved it onto the schema,
// an override-declared key is indistinguishable from a schema-declared one —
// which is the whole point of that pass.
//
// A config override may restate a key the schema already declares; §8.6 allows
// it ("the override silently takes precedence over any auto-detected PK"), and
// a composite restatement has a real use, since override order sets <Table>PK
// field order and cache-key order. Such a key is DB-backed and keeps its
// target: a table-level PRIMARY KEY leaves a Constraint behind, and the
// write-back never synthesizes one, so that constraint is a sound discriminator.
//
// KNOWN LIMITATION — an *inline* single-column `PRIMARY KEY` records only the
// column flag, on all three dialects (no Constraint entry). Once the write-back
// has set that same flag, `id uuid PRIMARY KEY` + `primary_key.columns: [id]`
// is byte-identical to an app-enforced override, so this returns false and the
// table loses <T>ConflictPK. It also loses its manifest `<table>_pkey`
// (pkDeclaredInSchema) and, with no UNIQUE beside it, Upsert and UpsertMany.
// The bias is deliberate: a missing constant is a compile error a Go caller
// sees immediately (the GraphQL flat upsert<T> is omitted instead), whereas a wrongly-emitted target is a 42P10 that only fires in
// production. Deleting the redundant config line restores the
// target; it buys nothing for a single-column key.
func pkConflictTargetIsIndexed(table *parser.Table, tableCfg config.TableConfig, pkCols []string) bool {
	return pkDeclaredInSchema(table, tableCfg, pkCols) || uniqueCoversColumns(table, pkCols)
}

// pkDeclaredInSchema reports whether the resolved primary key is a PRIMARY KEY
// the schema declared, and so one the database built a primary-key index for.
//
// It is pkConflictTargetIsIndexed's first half, on the same evidence: no
// primary_key.columns override, or an override restating a table-level PRIMARY
// KEY constraint. A key that is only backed by a set-equal UNIQUE has an index,
// but it is that UNIQUE's, listed under its own name. The manifest reads this
// through TableContext.PKDeclaredInSchema so that indexes[] names no
// primary-key index the database does not have (PRD §30.7). It carries the
// same known limitation: an inline PRIMARY KEY restated by the override reads
// as app-enforced.
func pkDeclaredInSchema(table *parser.Table, tableCfg config.TableConfig, pkCols []string) bool {
	if tableCfg.PrimaryKey == nil || len(tableCfg.PrimaryKey.Columns) == 0 {
		return true
	}
	return constraintCoversColumns(table, parser.PrimaryKey, pkCols)
}

// uniqueCoversColumns reports whether the table carries a UNIQUE constraint
// whose column membership is exactly cols (order-independent), or — for a
// single column — an inline UNIQUE on that column.
//
// Set equality is required, matching config.uniqueCovers: a UNIQUE on (a) does
// not make (a, b) conflict-targetable. Partial UNIQUE constraints (Where != "")
// are skipped for the same reason buildConflictTargets skips them — ON CONFLICT
// against a partial index requires the predicate, which the generated upsert
// surface does not model.
func uniqueCoversColumns(table *parser.Table, cols []string) bool {
	if len(cols) == 1 {
		if col := parser.ColumnByName(table, cols[0]); col != nil && col.Unique {
			return true
		}
	}
	return constraintCoversColumns(table, parser.Unique, cols)
}

// constraintCoversColumns reports whether the table carries a constraint of the
// given type whose column membership is exactly cols, order-independent.
//
// Partial constraints (Where != "") never count: ON CONFLICT against a partial
// index requires the predicate, which the generated upsert surface does not
// model. Only UNIQUE constraints can carry one, so the check is harmless for
// PRIMARY KEY.
func constraintCoversColumns(table *parser.Table, kind parser.ConstraintType, cols []string) bool {
	want := make(map[string]bool, len(cols))
	for _, name := range cols {
		want[name] = true
	}
	for _, c := range table.Constraints {
		if c.Type != kind || c.Where != "" || len(c.Columns) != len(want) {
			continue
		}
		covered := true
		for _, name := range c.Columns {
			if !want[name] {
				covered = false
				break
			}
		}
		if covered {
			return true
		}
	}
	return false
}

func conflictConstantSuffix(columns []string) string {
	var b strings.Builder
	for _, col := range columns {
		b.WriteString(toPascalCase(col))
	}
	return b.String()
}

// --- Increment columns ---

// buildIncrementColumns selects the columns eligible for atomic
// increment/decrement: numeric, and identifying nothing.
//
// Primary keys are excluded because a row's identity is not an amount. Foreign
// keys are excluded for the same reason one step removed — `category_id + 1`
// is arithmetic on *another* row's identity, and lands on whichever row
// happens to sit at the next value. There is no schema in which that is the
// intended operation, and an integer FK would otherwise pick up `_inc` / `_dec`
// operators on the generated update input purely because its Go type is an
// int. On a tenanted schema it is worse than meaningless: `Increment`
// constrains which rows it matches but not which column it targets, so
// incrementing an FK walks a reference into another tenant's row.
//
// Non-integer FKs (uuid, text) never reached here — this closes the gap for
// the integer ones, which is the only reason the distinction was invisible.
//
// The tenant column is excluded on the same grounds, one step further out: it
// identifies not another row but the whole partition the row belongs to, so
// `tenant_id + 1` walks the row itself into another tenant. The tenant
// predicate `Increment` appends to its WHERE does not protect the SET, and no
// pre-write check can, because the destination value is not known until after
// the arithmetic. This applies whatever `tenancy.required` resolves to —
// required:false's documented cross-tenant path names a target tenant through
// `Update`, which arithmetic cannot do (PRD §8.2, §29.4.2).
func buildIncrementColumns(columns []ColumnContext) []ColumnContext {
	var result []ColumnContext
	for _, col := range columns {
		if isIncrementEligible(col) {
			result = append(result, col)
		}
	}
	return result
}

// isIncrementEligible reports whether a resolved column is enrolled in the
// Increment surface: an arithmetic Go type over an arithmetic SQL column,
// identifying nothing. It is the single decision point — the client surface,
// the GraphQL `_inc` / `_dec` operators and the `_inc` namespace check all
// read it, so none of them can drift from the others.
//
// ColumnContext.Tenant is attached after the table context is assembled, so
// callers reached during assembly see it unset. buildIncrementColumns is
// therefore re-run once tenancy lands; see attachTenancyToTables.
func isIncrementEligible(col ColumnContext) bool {
	return !col.PrimaryKey && col.FKReference == nil && !col.Tenant &&
		isIncrementableGoType(col.GoType) && isArithmeticSQLColumn(col)
}

// isArithmeticSQLColumn reports whether the column's SQL type supports `+`.
// An unclassifiable type is trusted — see incrementSQLTypeWarnings.
func isArithmeticSQLColumn(col ColumnContext) bool {
	category := sqlTypeCategory(col.BaseSQLType)
	return category == categoryUnknown || category == arithmeticSQLCategory
}

// incrementBlockedBySQLType reports the one case worth warning about: a column
// that an override made arithmetic in Go, on a SQL type that is not. Columns
// excluded for identifying something, or never arithmetic to begin with, are
// ordinary non-increment columns and say nothing.
//
// The tenant exclusion is deliberately not mirrored here. incrementSQLTypeWarnings
// runs during buildSingleTableContext, before attachTenancyToTables stamps
// ColumnContext.Tenant, so testing it would be dead — and the warning is about
// the override, which is worth naming whatever else disqualifies the column.
func incrementBlockedBySQLType(col ColumnContext) bool {
	return !col.PrimaryKey && col.FKReference == nil &&
		isIncrementableGoType(col.GoType) && !isArithmeticSQLColumn(col)
}

// arithmeticSQLCategory names the sqlTypeCategory bucket the increment SQL
// (`column = column + ?`) is valid on. sqlTypeCategory already folds numeric,
// decimal, real and float in with the integer types, so that one bucket is
// exactly "supports +" — the existing vocabulary extended rather than a second
// classifier.
const arithmeticSQLCategory = categoryInteger

// parenIfComposite wraps a zero-value expression in parens when it is a Go
// composite literal (i.e. ends with `{}`). This is needed wherever the
// expression appears in `if` / `for` / `switch` headers — Go's grammar would
// otherwise consume the trailing `{` as the body's open brace and reject
// `if x != T{} {` with "expected ';', found '{'". For sentinel zero values
// (`0`, `""`, `nil`, `uuid.Nil`) the parens are unnecessary, so we leave the
// value untouched.
func parenIfComposite(zeroValue string) string {
	if strings.HasSuffix(zeroValue, "{}") {
		return "(" + zeroValue + ")"
	}
	return zeroValue
}

// isComparatorNumericGoType reports whether a Go type satisfies the
// `comparator.Numeric` constraint (`~int | … | ~float64`) — i.e. is eligible
// for the comparator-side `Number[T]` codepath. This set must NOT
// include `decimal.Decimal` (which does not satisfy the underlying type
// constraint); decimal columns route through the String comparator instead.
//
// For the broader "is this column eligible for Increment?" predicate, use
// isIncrementableGoType — Increment runs `column = column + ?` SQL at the
// dialect level and works on decimal as well as the comparator-numeric set.
func isComparatorNumericGoType(goType string) bool {
	base := strings.TrimPrefix(goType, "*")
	switch base {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"float32", "float64":
		return true
	case "sql.NullInt16", "sql.NullInt32", "sql.NullInt64", "sql.NullFloat64":
		return true
	}
	return false
}

// isIncrementableGoType reports whether a Go type is eligible for the
// runtime client's `Increment` method (and therefore for the API-side
// `Update<Table>` increment dispatch / `_inc` / `_dec` paired-field
// emission). Superset of isComparatorNumericGoType: additionally includes
// `decimal.Decimal` and its nullable / pointer forms — Increment's dialect
// SQL (`column = column + ?`) works on decimal even though decimal cannot
// satisfy `comparator.Numeric`.
func isIncrementableGoType(goType string) bool {
	if isComparatorNumericGoType(goType) {
		return true
	}
	switch strings.TrimPrefix(goType, "*") {
	case "decimal.Decimal", "decimal.NullDecimal":
		return true
	}
	return false
}

// --- Filter fields ---

func buildFilterFields(columns []ColumnContext, dialect config.Dialect, softDelete *SoftDeleteContext) []FilterFieldContext {
	softDeleteCol := ""
	if softDelete != nil {
		softDeleteCol = softDelete.Column
	}

	fields := make([]FilterFieldContext, 0, len(columns))
	for _, col := range columns {
		filterable := columnIsFilterable(col, dialect)
		cType := ""
		if filterable {
			cType = resolveComparatorType(col, dialect)
		}
		fields = append(fields, FilterFieldContext{
			FieldName:          col.FieldName,
			ComparatorType:     cType,
			ComparatorImport:   "github.com/teandresmith/sqlgen/comparator",
			ColumnName:         col.Name,
			IsSoftDeleteColumn: col.Name == softDeleteCol,
			Filterable:         filterable,
		})
	}
	slices.SortFunc(fields, func(a, b FilterFieldContext) int {
		return strings.Compare(a.FieldName, b.FieldName)
	})
	return fields
}

// resolveComparatorType determines the comparator type for a column
// following PRD 11.2 rules.
func resolveComparatorType(col ColumnContext, dialect config.Dialect) string {
	baseGoType := strings.TrimPrefix(col.GoType, "*")
	// Under overrides.use_pointers: false a nullable column arrives as a
	// database/sql wrapper rather than as *T. Classify it by the type it
	// wraps: the wrapper is how the value is stored, not what it is compared
	// as. Without this a nullable `double precision` column asked for
	// comparator.NullableNumber[sql.NullFloat64] — which does not satisfy
	// comparator.Numeric, so the package did not compile — and a nullable
	// `boolean` or `timestamptz` column fell through to a string comparator.
	// col.GoType is left alone: the predicates below and the
	// struct field keep the wrapper.
	if underlying, ok := gotype.StdNullUnderlying(baseGoType); ok {
		baseGoType = underlying
	}

	if generic := resolveGenericComparator(col, baseGoType); generic != "" {
		return generic
	}

	base := resolveSimpleComparator(col, baseGoType, dialect)
	if col.Nullable {
		return "*comparator.Nullable" + base
	}
	return "*comparator." + base
}

func resolveGenericComparator(col ColumnContext, baseGoType string) string {
	switch {
	case col.IsSet:
		// SET columns store comma-separated strings; use String comparator.
		return ""
	case col.IsSlice:
		elemType := sliceElementType(col, baseGoType)
		if !isComparableSliceElement(elemType) {
			// `comparator.Slice[T]` requires `T comparable`. JSON
			// element types (`types.JSON`, `json.RawMessage`,
			// `map[string]any`) are slices/maps and violate the constraint.
			// Signal "no comparator" so the caller marks the column
			// non-filterable; templates skip it.
			return ""
		}
		return formatGenericComparator("Slice", elemType, col.Nullable)
	case isEnumLikeType(baseGoType):
		return formatGenericComparator("Enum", baseGoType, col.Nullable)
	case baseGoType == "time.Duration":
		// PRD §11.2: `time.Duration` is `~int64`, so it satisfies
		// comparator.Numeric and needs no family of its own — the driver
		// encodes it as an interval, so the ordered and Between operators
		// compare intervals rather than nanosecond counts.
		//
		// This arm exists so the classification stays comparator-only.
		// Routing it through isComparatorNumericGoType instead would ALSO
		// widen isIncrementableGoType, and sqlTypeCategory("interval") is
		// categoryUnknown — which isArithmeticSQLColumn trusts — so an
		// interval column would become increment-eligible and emit
		// `dur_inc: Int` / `dur_dec: Int` on the update input. That is the
		// same operand mismatch comparator.Opaque exists to remove.
		return formatGenericComparator("Number", baseGoType, col.Nullable)
	case isComparatorNumericGoType(col.GoType):
		return formatGenericComparator("Number", baseGoType, col.Nullable)
	case isOpaqueComparatorGoType(baseGoType):
		return formatGenericComparator("Opaque", baseGoType, col.Nullable)
	}
	return ""
}

// pkComparatorFamily classifies which comparator family a PK or FK column's
// filter field takes, given its Go type. Returns the family's base name
// ("ID", "Number", "Opaque") and, for the two generic families, the type
// parameter.
//
// This exists so the generated client's own PK-filter expressions
// (funcPKFilterExpr / pkFilterInExpr / funcPKIsStringType in funcmap.go) are
// derived from the SAME classification as the filter field they assign into,
// rather than re-deriving it from isComparatorNumericGoType alone. That
// re-derivation was a third independent spelling of the fact
// resolveComparatorType owns, and it broke: once `[]byte` and the
// `net` types started resolving to comparator.Opaque[T], a `BLOB PRIMARY KEY`
// still had `&comparator.ID{…}` assigned into its `*comparator.Opaque[[]byte]`
// field and the generated package stopped compiling. The arms below mirror
// resolveGenericComparator's order exactly; the two must move together.
//
// Only the arms reachable for a key column are modelled: IsSet / IsSlice /
// enum-like columns keep whatever resolveGenericComparator gives them and are
// not routed here, because the helpers this serves are called only for PK and
// FK columns.
func pkComparatorFamily(goType string) (family, typeParam string) {
	base := strings.TrimPrefix(goType, "*")
	if underlying, ok := gotype.StdNullUnderlying(base); ok {
		base = underlying
	}
	switch {
	case base == durationGoType:
		return "Number", base
	case isComparatorNumericGoType(goType):
		return "Number", base
	case isOpaqueComparatorGoType(base):
		return "Opaque", base
	}
	return "ID", ""
}

// isOpaqueComparatorGoType reports whether a Go type filters through
// `comparator.Opaque[T]` (PRD §11.2). These are the four types the built-in
// dialect tables resolve binary and network columns to: each is equality- and
// order-comparable in SQL, but none is a Go `string` and none satisfies
// comparator.Numeric, so without this arm they would fall through
// resolveSimpleComparator's `default: return "String"` arm and the generated
// filter would compare a binary or network column against a text parameter.
//
// Deliberately keyed on the exact resolved Go type rather than on the SQL type:
// a column retyped through `type_map` / `overrides.types` should follow its new
// Go type, and a consumer type that merely *looks* binary is not in this set —
// the String fallback is correct for those (§11.2, "Types not named above").
func isOpaqueComparatorGoType(goType string) bool {
	switch goType {
	case "[]byte", "net.IP", "net.IPNet", "net.HardwareAddr":
		return true
	}
	return false
}

// columnIsFilterable reports whether the column can participate in the
// generated filter struct. Two shapes are excluded.
//
// Columns whose Go type cannot satisfy the comparator package's
// `T comparable` constraint — currently `json[]` and `jsonb[]` array columns,
// which resolve to `[]types.JSON` (i.e. `[]json.RawMessage` =
// `[][]byte`). Filters on JSON-array columns are not useful either way:
// PostgreSQL's array operators on jsonb are limited and semantically murky,
// and consumers needing them should drop to raw SQL.
//
// And JSON columns under SQLite. `comparator.JSON.Parse` switches
// on the dialect and carries `postgres` and `mysql` arms only, so on SQLite
// both of its operators fall through and the comparator contributes zero
// conditions — a filter field that parses, dispatches into a real translator
// and emits no SQL. Gating here rather than in the GraphQL projection is what
// makes the two surfaces agree: the projection reads the same
// resolveComparatorType this arm keys on, so the Go filter field, the GraphQL
// input field and the translator entry appear and disappear together. PRD
// §11.2 defers SQLite JSON support; adding a `sqlite` arm to Parse later
// re-enables the family here with no other change.
//
// The dialect is a parameter rather than read from the column because
// ColumnContext carries the SQL type, not the dialect that spelled it.
func columnIsFilterable(col ColumnContext, dialect config.Dialect) bool {
	if col.IsSlice {
		elemType := sliceElementType(col, strings.TrimPrefix(col.GoType, "*"))
		return isComparableSliceElement(elemType)
	}
	switch resolveComparatorType(col, dialect) {
	case "*comparator.JSON", "*comparator.NullableJSON":
		return dialect != config.DialectSQLite
	}
	return true
}

// sliceElementType returns the element type of a slice column, preferring
// SliceElemType (populated for named enum slices) and falling back to
// stripping the `[]` prefix from the column's bare Go type.
func sliceElementType(col ColumnContext, baseGoType string) string {
	if col.SliceElemType != "" {
		return col.SliceElemType
	}
	return strings.TrimPrefix(baseGoType, "[]")
}

// isComparableSliceElement reports whether the element type of a Go slice
// satisfies the `T comparable` constraint required by `comparator.Slice[T]`
// (`comparator/slice.go`). Slice / map element types (`types.JSON`,
// `json.RawMessage`, `map[string]any`) do not.
func isComparableSliceElement(elemType string) bool {
	switch elemType {
	case "types.JSON", "json.RawMessage", "map[string]any":
		return false
	}
	return true
}

func formatGenericComparator(base, typeParam string, nullable bool) string {
	if nullable {
		return fmt.Sprintf("*comparator.Nullable%s[%s]", base, typeParam)
	}
	return fmt.Sprintf("*comparator.%s[%s]", base, typeParam)
}

func resolveSimpleComparator(col ColumnContext, baseGoType string, dialect config.Dialect) string {
	switch {
	case (col.PrimaryKey || col.FKReference != nil) && (baseGoType == "string" || col.FKConvert != gotype.FKStringNone):
		return "ID"
	case baseGoType == "string":
		return "String"
	case baseGoType == "bool":
		return "Bool"
	case baseGoType == "time.Time", baseGoType == "types.DateTime", baseGoType == "types.NullDateTime":
		return "Time"
	case baseGoType == "map[string]any", baseGoType == "types.JSON":
		// json/jsonb columns resolve to types.JSON via gotype (PRD §7.6); the original map[string]any arm stays for overrides or
		// direct configs that map a column to the bare Go type.
		if dialect == config.DialectPostgres && strings.EqualFold(col.SQLType, "jsonb") {
			return "JSONB"
		}
		return "JSON"
	case strings.EqualFold(col.SQLType, "uuid"):
		// A bare `uuid` column that is neither PK nor FK (e.g. a polymorphic
		// soft-reference like activity.entity_id, or a uuid carried through a
		// view) still filters as an identifier. GraphQL maps uuid to the ID
		// scalar → IDComparator; without this arm the runtime would emit
		// comparator.String, so the generated filter translator would try to
		// pass an *IDComparator into translateStringComparator and fail to
		// compile. Keyed on the SQL type (not FKConvert, which is also set for
		// decimal/time and would misclassify them) and placed after the
		// concrete Go-type arms.
		return "ID"
	default:
		return "String"
	}
}

func isEnumLikeType(goType string) bool {
	if goType == "" || strings.Contains(goType, ".") {
		return false
	}
	r := rune(goType[0])
	if r < 'A' || r > 'Z' {
		return false
	}
	switch goType {
	case "string", "bool", "byte", "rune",
		"int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"float32", "float64":
		return false
	}
	return true
}

// --- Input field classification ---

func omittableInputField(col ColumnContext) InputFieldContext {
	omitType, omitImport := wrapOmittable(col.GoType, col.Import)
	return InputFieldContext{
		FieldName:   col.FieldName,
		GoType:      omitType,
		Import:      omitImport,
		ColumnName:  col.Name,
		Omittable:   true,
		Description: col.Description,
		JSONTag:     col.Name,
		DefaultExpr: col.DefaultExpr,
	}
}

func requiredInputField(col ColumnContext) InputFieldContext {
	return InputFieldContext{
		FieldName:   col.FieldName,
		GoType:      col.GoType,
		Import:      col.Import,
		ColumnName:  col.Name,
		Required:    true,
		Description: col.Description,
		JSONTag:     col.Name,
	}
}

func buildCreateInputFields(columns []ColumnContext, pkStrategy config.PKStrategy, updateColumns []string, softDelete *SoftDeleteContext) []InputFieldContext {
	updateSet := make(map[string]bool, len(updateColumns))
	for _, c := range updateColumns {
		updateSet[c] = true
	}

	softDeleteCol := ""
	if softDelete != nil {
		softDeleteCol = softDelete.Column
	}

	var fields []InputFieldContext
	for _, col := range columns {
		if col.IsComputed {
			continue
		}

		// PK classification: auto-increment excluded, UUID omittable, caller required.
		if col.PrimaryKey {
			switch pkStrategy {
			case config.PKStrategyDB:
				if col.AutoIncrement || isSerialType(strings.ToLower(col.SQLType)) {
					continue
				}
				fields = append(fields, omittableInputField(col))
			case config.PKStrategyApp:
				fields = append(fields, omittableInputField(col))
			case config.PKStrategyCaller:
				fields = append(fields, requiredInputField(col))
			}
			continue
		}

		// Update columns and soft delete columns → omittable (supports data backfilling).
		if updateSet[col.Name] || col.Name == softDeleteCol || col.Nullable || col.HasDefault {
			fields = append(fields, omittableInputField(col))
		} else {
			fields = append(fields, requiredInputField(col))
		}
	}
	return fields
}

func buildUpdateInputFields(columns []ColumnContext, _ []string, _ *SoftDeleteContext) []InputFieldContext {
	var fields []InputFieldContext
	for _, col := range columns {
		if col.PrimaryKey || col.IsComputed {
			continue
		}
		fields = append(fields, omittableInputField(col))
	}
	return fields
}

func wrapOmittable(goType, _ string) (string, string) {
	return fmt.Sprintf("omittable.Value[%s]", goType), "github.com/teandresmith/sqlgen/omittable"
}

// --- Scan shapes ---

func buildScanShapes(columns []ColumnContext, driver config.Driver, structName string) []ScanShapeContext {
	varName := strings.ToLower(structName[:1])
	shapes := make([]ScanShapeContext, 0, len(columns))
	for _, col := range columns {
		shapes = append(shapes, buildSingleScanShape(col, driver, varName))
	}
	return shapes
}

func buildSingleScanShape(col ColumnContext, driver config.Driver, varName string) ScanShapeContext {
	shape := ScanShapeContext{
		ColumnName: col.Name,
		FieldName:  col.FieldName,
	}

	if col.IsSlice && driver == config.DriverStdlib && col.SliceElemType == "" {
		// Bare slices ([]string, []int) need pq.Array wrapping for stdlib.
		// Named enum slices (UserRoleSlice) have their own Scan/Value methods.
		shape.Shape = "wrapped"
		shape.ScanExpr = fmt.Sprintf("pq.Array(&%s.%s)", varName, col.FieldName)
		return shape
	}

	shape.Shape = "direct"
	shape.ScanExpr = fmt.Sprintf("&%s.%s", varName, col.FieldName)
	return shape
}

// --- RelationshipOptions definitions ---

// buildRelationshipTargetClients returns the deduped list of target struct
// names for O2M/M2M relationships, sorted alphabetically. Multiple
// relationships into the same target table (PRD §13.7 sub-categorized
// polymorphism) collapse to a single entry so the entity-client struct emits
// one `<target>Client` field instead of N.
func buildRelationshipTargetClients(o2m, m2m []RelationshipContext) []string {
	seen := make(map[string]bool, len(o2m)+len(m2m))
	out := make([]string, 0, len(o2m)+len(m2m))
	for _, rel := range o2m {
		if !seen[rel.TargetStructName] {
			seen[rel.TargetStructName] = true
			out = append(out, rel.TargetStructName)
		}
	}
	for _, rel := range m2m {
		if !seen[rel.TargetStructName] {
			seen[rel.TargetStructName] = true
			out = append(out, rel.TargetStructName)
		}
	}
	slices.Sort(out)
	return out
}

// buildRelationshipOptionsDefs returns unique RelationshipOptions struct
// definitions needed for O2M/M2M relationships, sorted by struct name.
func buildRelationshipOptionsDefs(rels []RelationshipContext) []RelationshipOptionsDef {
	seen := make(map[string]bool)
	var defs []RelationshipOptionsDef
	for _, rel := range rels {
		if rel.Type == parser.OneToOne {
			continue
		}
		structName := rel.TargetStructName + "RelationshipOptions"
		if seen[structName] {
			continue
		}
		seen[structName] = true
		defs = append(defs, RelationshipOptionsDef{
			StructName:       structName,
			TargetStructName: rel.TargetStructName,
		})
	}
	slices.SortFunc(defs, func(a, b RelationshipOptionsDef) int {
		return strings.Compare(a.StructName, b.StructName)
	})
	return defs
}

// --- All column names ---

// buildAllColumnNames returns a sorted list of all SQL column names.
func buildAllColumnNames(columns []ColumnContext) []string {
	names := make([]string, len(columns))
	for i, col := range columns {
		names[i] = col.Name
	}
	slices.Sort(names)
	return names
}

// --- O2O join details ---

// buildO2OJoinDetails builds the O2O join detail tree for the relationships template.
// It walks O2O relationships, resolves target table metadata, and discovers chains.
// parentPKColumn is the SQL name of the parent table's first PK column, which a
// has-one edge joins its target's FK to. Every table that reaches here has one:
// BuildTableContexts skips a table with no resolved PK.
func buildO2OJoinDetails(input *GenerateInput, o2oRels []RelationshipContext, parentAlias, parentPKColumn string) (details []O2OJoinDetail, allTargets []O2OJoinDetail, err error) {
	usedAliases := map[string]bool{parentAlias: true}

	for _, rel := range o2oRels {
		detail, derr := buildSingleO2ODetail(input, rel, parentAlias, parentPKColumn, usedAliases, parentAlias, "fo", 0)
		if derr != nil {
			return nil, nil, derr
		}
		if detail == nil {
			continue
		}
		details = append(details, *detail)
		allTargets = append(allTargets, *detail)
		allTargets = append(allTargets, flattenChainedJoins(detail.ChainedJoins)...)
	}

	slices.SortFunc(details, func(a, b O2OJoinDetail) int {
		return strings.Compare(a.Alias, b.Alias)
	})
	slices.SortFunc(allTargets, func(a, b O2OJoinDetail) int {
		return strings.Compare(a.Alias, b.Alias)
	})
	return details, allTargets, nil
}

// buildSingleO2ODetail builds one O2O join detail, recursively discovering chains.
// parentFOPath is the FieldOptions path up to the parent (e.g., "fo" or "fo.Users").
// parentPKColumn is the SQL name of the parent's first PK column — the
// table joined under parentAlias, which is the previous hop's target on a
// chain.
func buildSingleO2ODetail(input *GenerateInput, rel RelationshipContext, parentAlias, parentPKColumn string, usedAliases map[string]bool, parentVarName string, parentFOPath string, depth int) (*O2OJoinDetail, error) {
	if depth > 5 {
		return nil, nil // guard against excessive chain depth
	}

	targetTable := findSchemaTable(input.Schema, rel.TargetSchema, rel.TargetTable)
	if targetTable == nil {
		return nil, nil
	}
	targetCfg := resolveTableConfig(input.Config.Tables, targetTable.Schema, rel.TargetTable)

	alias := generateAlias(rel.FieldName, usedAliases)
	varName := alias

	// Find target table PK
	var pkCol *ColumnContext
	targetColumns, targetPKColumns := buildTableColumns(input, targetTable, targetCfg)
	if len(targetPKColumns) > 0 {
		pkCol = &targetPKColumns[0]
	}
	if pkCol == nil {
		return nil, nil
	}

	// Detect soft delete on target
	targetSoftDelete, _ := detectTableSoftDelete(targetTable, input.Config, targetColumns)

	// Build scan cases for target columns
	scanCases := buildO2OScanCases(targetColumns, alias, varName, pkCol.Name, input.Config.Output.Driver)

	// Build FieldOptions access expressions
	foPath := parentFOPath + "." + rel.FieldName
	foCheck := foPath + " != nil"
	foColumns := foPath + ".Columns()"

	// Determine which side the FK column is on. For auto-detected O2O,
	// the FK is on the parent (source) table. For config-defined O2O,
	// the FK may be on the target table instead. When the FK is on the
	// target, swap the ON clause sides. The fact itself is derived once, in
	// fkColumnOnTarget, and read here (PRD §13.1).
	//
	// The swapped parent side is the parent's own PK, the column a nested
	// has-one create writes into the FK (PRD §9.9.1 shape 2) — not pkCol,
	// which is the target's and only coincides when both tables spell their
	// key the same. pkCol stays the target-side scan / HasData key.
	onLocal := rel.FKColumn // FK column (on parent by default)
	onRemote := pkCol.Name  // PK column (on target by default)
	if rel.FKOnTarget {
		onLocal = parentPKColumn // PK on the parent
		onRemote = rel.FKColumn  // FK on the target
	}

	qualifiedFilter, err := o2oJoinPredicate(rel, alias, input.Config.Input.Dialect)
	if err != nil {
		return nil, err
	}

	detail := O2OJoinDetail{
		FieldOptionsCheck:   foCheck,
		FieldOptionsColumns: foColumns,
		TargetTable:         targetTable.Name,
		TargetSchema:        targetTable.Schema,
		Alias:               alias,
		OnLocalAlias:        parentAlias,
		OnLocal:             onLocal,
		OnRemote:            onRemote,
		Filter:              qualifiedFilter,
		StructName:          rel.TargetStructName,
		VarName:             varName,
		HasDataVar:          varName + "HasData",
		PKFieldName:         pkCol.FieldName,
		PKColumn:            pkCol.Name,
		PKZeroValue:         parenIfComposite(pkCol.ZeroValue),
		ParentVarName:       parentVarName,
		FieldName:           rel.FieldName,
		ScanCases:           scanCases,
	}

	if targetSoftDelete != nil {
		detail.HasSoftDelete = true
		detail.SoftDeleteColumn = targetSoftDelete.Column
		detail.SoftDeleteTypeConst = funcSoftDeleteTypeConst(targetSoftDelete.Strategy)
	}

	// Discover chained O2O relationships on the target table
	targetRels := findO2ORelationships(input.Schema, targetTable, targetCfg, input.relTargets)
	for _, chainRel := range targetRels {
		chained, err := buildSingleO2ODetail(input, chainRel.rel, alias, pkCol.Name, usedAliases, varName, foPath, depth+1)
		if err != nil {
			return nil, err
		}
		if chained != nil {
			detail.ChainedJoins = append(detail.ChainedJoins, *chained)
		}
	}

	return &detail, nil
}

// chainedRelInfo wraps a relationship context for chain discovery.
type chainedRelInfo struct {
	rel RelationshipContext
}

// findO2ORelationships finds O2O relationships for a target table.
func findO2ORelationships(schema *parser.Schema, table *parser.Table, tableCfg config.TableConfig, targets *relationshipTargets) []chainedRelInfo {
	excludeSet := make(map[string]bool)
	for _, name := range tableCfg.ExcludeRelationships {
		excludeSet[name] = true
	}

	qualifiedName := table.Name
	if table.Schema != "" {
		qualifiedName = table.Schema + "." + table.Name
	}

	var results []chainedRelInfo
	for _, r := range schema.Relationships {
		if r.SourceTable != qualifiedName || r.Type != parser.OneToOne {
			continue
		}
		if excludeSet[r.Name] {
			continue
		}
		if !targets.generates(r.TargetTable) {
			continue
		}
		results = append(results, chainedRelInfo{
			rel: relationshipToContext(schema, r, targets),
		})
	}

	for _, r := range tableCfg.Relationships {
		relType := parseRelationshipType(r.Type)
		if relType != parser.OneToOne {
			continue
		}
		if excludeSet[r.Name] {
			continue
		}
		// Skipped rather than reported: this table's own build reports the
		// same edge, since a chained target is itself a generated table.
		if checkDeclaredTarget(r, targets, "") != nil {
			continue
		}
		results = append(results, chainedRelInfo{
			rel: configRelationshipToContext(schema, r, targets),
		})
	}
	return results
}

// o2oJoinPredicate returns the alias-qualified sub-categorization predicate an
// O2O JOIN ON clause carries, or "" when the edge declares none.
//
// Bare identifiers are qualified to the target alias so a multi-column filter
// binds every column to the target table instead of the parent;
// already-qualified refs are left untouched by the parser-driven rewrite.
//
// A `discriminator:` goes through the *same* qualifier rather than being
// assembled by hand, because PRD §13.4.1 defines it by equivalence to the
// `filter:` it replaces and only the dialect's own parser reproduces that
// spelling: pg_query and vitess emit bare identifiers where rqlite/sql emits
// `"alias"."column"`. The two forms are mutually exclusive (config validation
// rejects an edge declaring both), so the order of the branches below is not
// load-bearing.
func o2oJoinPredicate(rel RelationshipContext, alias string, dialect config.Dialect) (string, error) {
	predicate := strings.TrimSpace(rel.Filter)
	label := "filter"
	if rel.Discriminator != nil {
		predicate = rel.Discriminator.EquivalentFilter()
		label = "discriminator"
	}
	if predicate == "" {
		return "", nil
	}
	qualified, err := qualifyFilter(predicate, alias, dialect)
	if err != nil {
		return "", fmt.Errorf("relationship %s: %s %q: %w", rel.FieldName, label, predicate, err)
	}
	return qualified, nil
}

// findSchemaTable finds a table by schema and name. If targetSchema is non-empty,
// it matches exactly; otherwise it returns the first table with the given name.
func findSchemaTable(schema *parser.Schema, targetSchema, name string) *parser.Table {
	if targetSchema != "" {
		for i := range schema.Tables {
			if schema.Tables[i].Schema == targetSchema && schema.Tables[i].Name == name {
				return &schema.Tables[i]
			}
		}
		return nil
	}
	for i := range schema.Tables {
		if schema.Tables[i].Name == name {
			return &schema.Tables[i]
		}
	}
	return nil
}

// generateAlias returns a unique O2O JOIN alias for the relationship field
// name: its first letter, lowercased, and when that is taken, the letter
// followed by a counter from 2 (`u`, `u2`, `u3`).
//
// The alias is written bare in SQL (PRD §13.2) and declared as a Go local in
// the scanner, so it must be neither a keyword nor a name the scanner already
// uses. A growing prefix of the field name was both: `in`, `use`, `user` and
// `go`. No SQL keyword in any supported dialect, no Go keyword and no
// predeclared identifier is a single letter or a letter followed by digits,
// and neither shape can collide with the scanner's own locals (`result`,
// `targets`, `idx`, `col`, `rows`, `err`).
func generateAlias(name string, used map[string]bool) string {
	letter := strings.ToLower(name[:1])
	candidate := letter
	for i := 2; used[candidate]; i++ {
		candidate = letter + strconv.Itoa(i)
	}
	used[candidate] = true
	return candidate
}

// buildO2OScanCases builds scan switch cases for a target table's columns in a JOIN context.
//
// Unlike the parent's cases, every destination here has to survive a LEFT JOIN
// miss: the miss is detected *after* the scan, by testing the target's PK
// against its zero value, and on a miss the driver returns NULL for each of the
// target's columns. A NOT NULL column resolves to a Go type that refuses NULL,
// so the scan failed before the detection could run and the whole read errored
// for any parent without a matching row. Those
// destinations are wrapped in [database.NullScan], which reads NULL as the zero
// value and leaves the decision to the miss detection.
//
// Columns the schema already declares nullable resolve to a type that takes
// NULL on its own, and a slice column reads a NULL array as a nil slice on both
// drivers, so neither is wrapped. Slices are excluded on *every* driver, not
// just the pq.Array arm below: wrapping makes the destination a
// [database/sql.Scanner], which drops pgx off its native array codec onto
// DecodeDatabaseSQLValue, and that hands the wire bytes to convertAssign —
// "unsupported Scan, storing driver.Value type []uint8 into type *[]string".
func buildO2OScanCases(columns []ColumnContext, alias, varName, pkColName string, driver config.Driver) []O2OScanCase {
	cases := make([]O2OScanCase, 0, len(columns))
	for _, col := range columns {
		sc := O2OScanCase{
			PrefixedColumn: alias + "." + col.Name,
			IsPK:           col.Name == pkColName,
			FieldName:      col.FieldName,
			VarName:        varName,
		}

		if col.IsSlice && driver == config.DriverStdlib && col.SliceElemType == "" {
			sc.Shape = "wrapped"
			sc.ScanExpr = fmt.Sprintf("pq.Array(&%s.%s)", varName, col.FieldName)
		} else {
			sc.Shape = "direct"
			sc.ScanExpr = nullSafeO2OScanExpr(fmt.Sprintf("&%s.%s", varName, col.FieldName), col.Nullable || col.IsSlice)
		}

		cases = append(cases, sc)
	}
	return cases
}

// nullSafeO2OScanExpr wraps an O2O target's scan destination so a LEFT JOIN
// miss reads as the zero value rather than failing the scan. A destination that
// already takes NULL is returned unchanged — wrapping one buys nothing and, for
// a slice under pgx, costs the native array codec.
func nullSafeO2OScanExpr(addrExpr string, takesNull bool) string {
	if takesNull {
		return addrExpr
	}
	return "database.NullScan(" + addrExpr + ")"
}

// buildO2OParentScanCases builds parent table scan cases with alias prefix.
func buildO2OParentScanCases(columns []ColumnContext, alias, varName string, driver config.Driver) []O2OScanCase {
	cases := make([]O2OScanCase, 0, len(columns))
	for _, col := range columns {
		sc := O2OScanCase{
			PrefixedColumn: alias + "." + col.Name,
			FieldName:      col.FieldName,
			VarName:        varName,
		}

		if col.IsSlice && driver == config.DriverStdlib && col.SliceElemType == "" {
			sc.Shape = "wrapped"
			sc.ScanExpr = fmt.Sprintf("pq.Array(&%s.%s)", varName, col.FieldName)
		} else {
			sc.Shape = "direct"
			sc.ScanExpr = fmt.Sprintf("&%s.%s", varName, col.FieldName)
		}

		cases = append(cases, sc)
	}
	return cases
}

// flattenChainedJoins flattens a tree of chained joins into a flat list.
func flattenChainedJoins(joins []O2OJoinDetail) []O2OJoinDetail {
	result := make([]O2OJoinDetail, 0, len(joins))
	for _, j := range joins {
		result = append(result, j)
		result = append(result, flattenChainedJoins(j.ChainedJoins)...)
	}
	return result
}

// reverseO2OTargets returns O2OAllTargets in reverse order for assignment.
func reverseO2OTargets(targets []O2OJoinDetail) []O2OJoinDetail {
	reversed := make([]O2OJoinDetail, len(targets))
	for i, t := range targets {
		reversed[len(targets)-1-i] = t
	}
	return reversed
}

// --- Relationship classification ---

// classifyRelationships splits relationships into O2O, O2M, and M2M slices.
func classifyRelationships(rels []RelationshipContext) (o2o, o2m, m2m []RelationshipContext) {
	for _, rel := range rels {
		switch rel.Type {
		case parser.OneToOne:
			o2o = append(o2o, rel)
		case parser.OneToMany:
			o2m = append(o2m, rel)
		case parser.ManyToMany:
			m2m = append(m2m, rel)
		}
	}
	return o2o, o2m, m2m
}

// applyConfigDeclaredFKs marks every column referenced by a config-declared
// relationship `fk:` as a foreign-key column when it has no SQL-level
// REFERENCES clause of its own. The mutation is required so the target
// table's filter struct types the FK column with the ID-style comparator that
// the relationship loader emits — without it, polymorphic FK columns (PRD
// §13.7, e.g. `documents.entity_id` with no REFERENCES because it can target
// multiple parents) would surface as `*comparator.String`/`*comparator.Number`
// while the loader passes `&comparator.ID{In: ...}`, producing a compile-time
// type mismatch in the generated client. The synthetic FK reference's
// destination (Table / Schema) is set to the declaring parent for traceability
// and is read by nothing — for comparator typing, only the existence of the
// reference matters.
//
// Every reference minted here is flagged Synthetic, and that flag *is* read.
// `fk` names a column the config author asserts is a foreign key, which is not
// the same as the schema declaring one: markSyntheticFK resolves it against the
// *related* table and will mark any column matching the name, including that
// table's own surrogate primary key. So a consumer reasoning about the
// referential contract rather than about comparator typing must skip these —
// see autoDetectPKStrategy, where treating one as real demotes a `db` PK to
// `caller`.
//
// Ordering matters and is not enforced by the type system: the CLI runs
// cli.applyPrimaryKeyOverrides and then parser.DetectRelationships on this
// schema *before* handing it to gen, so relationship classification sees the
// resolved primary key but never the references stamped here. A
// synthetic reference on a sole-PK column would otherwise promote that edge
// to a spurious O2O (parser.columnUnique).
func applyConfigDeclaredFKs(schema *parser.Schema, cfg *config.RootConfig) {
	if schema == nil || cfg == nil {
		return
	}
	for parentKey, tblCfg := range cfg.Tables {
		parentSchema, parentName := splitQualifiedName(parentKey)
		for _, rel := range tblCfg.Relationships {
			if rel.Table != "" && rel.FK != "" {
				markSyntheticFK(schema, rel.Table, rel.FK, parentSchema, parentName)
			}
		}
	}
}

func markSyntheticFK(schema *parser.Schema, targetTable, fkCol, parentSchema, parentName string) {
	tgtSchema, tgtName := splitQualifiedName(targetTable)
	for i := range schema.Tables {
		t := &schema.Tables[i]
		if t.Name != tgtName {
			continue
		}
		if tgtSchema != "" && t.Schema != tgtSchema {
			continue
		}
		for j := range t.Columns {
			col := &t.Columns[j]
			if col.Name != fkCol || col.FKReference != nil {
				continue
			}
			col.FKReference = &parser.FKReference{
				Table:     parentName,
				Schema:    parentSchema,
				Synthetic: true,
			}
		}
	}
}

// splitQualifiedName splits "schema.name" into (schema, name).
// If there's no dot, returns ("", name).
func splitQualifiedName(qualified string) (string, string) {
	if schema, name, ok := strings.Cut(qualified, "."); ok {
		return schema, name
	}
	return "", qualified
}

// resolveTableConfig looks up a TableConfig from the config map.
// It tries the schema-qualified key ("schema.name") first, then falls back
// to the bare table name. This allows config keys to be either qualified
// (e.g., "audit.users") or bare (e.g., "users").
//
// Reading cfg.Tables directly misses a qualified key, so a table configured
// under "public.users" resolves to an empty config and silently loses every
// override on it (PRD §5.5 "the schema prefix is optional in config keys").
// Every lookup goes through here for that reason.
func resolveTableConfig(tables map[string]config.TableConfig, schema, name string) config.TableConfig {
	if schema != "" {
		if cfg, ok := tables[schema+"."+name]; ok {
			return cfg
		}
	}
	return tables[name]
}

// resolveViewConfig looks up a ViewConfig from the config map.
// It tries the schema-qualified key first, then falls back to the bare name.
func resolveViewConfig(views map[string]config.ViewConfig, schema, name string) (config.ViewConfig, bool) {
	if schema != "" {
		if cfg, ok := views[schema+"."+name]; ok {
			return cfg, true
		}
	}
	cfg, ok := views[name]
	return cfg, ok
}
