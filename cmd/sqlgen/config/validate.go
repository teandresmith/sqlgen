package config

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

// Warning represents a non-fatal validation issue.
type Warning struct {
	Message string
}

// ValidatePreParse validates the config before schema parsing.
// It returns all warnings and all errors aggregated together so the
// consumer can fix them in one pass.
func ValidatePreParse(cfg *RootConfig) ([]Warning, error) {
	var warnings []Warning
	var errs []error

	validateEnumValues(cfg, &errs)
	validateDriverDialect(cfg, &errs)
	validateConnectionRequired(cfg, &errs)
	validateRemovedOperations(cfg, &errs)
	validateNullableVariants(cfg, &errs)
	validateSchemaDialect(cfg, &warnings)
	validateOutputDir(cfg, &warnings)
	validateCacheConfig(cfg, &errs, &warnings)
	validateViewCacheConfig(cfg, &errs)
	validateTenancyConfig(cfg, &errs)
	validateAPIConfig(cfg, &errs)
	validateAPIOperations(cfg, &errs)
	// Registered independently of validateAPIOperations, which returns early
	// when cfg.API is nil: a `views.<n>.api` block is just as wrong with no
	// top-level `api:` block as with one, and silently accepting it there is
	// the ignored-knob outcome both rules exist to prevent.
	validateViewAPIOperations(cfg, &errs)
	validateViewAPIOptIn(cfg, &errs)
	validateRelationships(cfg, &errs)
	validateNestedMutations(cfg, &errs)
	validateManifestConfig(cfg, &errs)
	validateColumnAccess(cfg, &errs)
	validateColumnOverrides(cfg, &errs)

	return warnings, errors.Join(errs...)
}

// validateOutputDir warns when output.dir was omitted and defaulted to the
// working directory (PRD §4.2). Generating into the module root is legal and
// occasionally intended, but it is far more often a key that never landed —
// output.dir is the anchor for every generated file and for the manifest
// stale-sweep, so a silent "." takes both to wherever sqlgen happens to be
// invoked from. An explicit `dir: "."` is deliberate and says
// nothing; only the defaulted form warns.
func validateOutputDir(cfg *RootConfig, warnings *[]Warning) {
	if !cfg.Output.dirDefaulted {
		return
	}
	*warnings = append(*warnings, Warning{
		Message: "output.dir is not set — generating into the current working directory. Set output.dir explicitly (e.g. output.dir: ./models); set it to \".\" to keep the module root and silence this warning (PRD §4.2)",
	})
}

// validateColumnAccess enforces the config-phase §32.4 access rules: the
// role must be one of the five known values, and classifying a column that
// exclude_columns removes is an error — nothing survives to classify.
// Column existence and the PK / required-on-create / soft-delete rules need
// the parsed schema and live in validateColumnAccessSchema.
func validateColumnAccess(cfg *RootConfig, errs *[]error) {
	for _, tableName := range sortedTableKeys(cfg.Tables) {
		table := cfg.Tables[tableName]
		if len(table.ColumnMap) == 0 {
			continue
		}
		excluded := make(map[string]bool)
		for _, c := range ResolveTableExcludeColumns(table, cfg.Generation) {
			excluded[c] = true
		}
		for _, colName := range slices.Sorted(maps.Keys(table.ColumnMap)) {
			access := table.ColumnMap[colName].Access
			if access == "" {
				continue
			}
			if !IsValidAccessRole(access) {
				*errs = append(*errs, fmt.Errorf(
					"tables.%s.column_map.%s.access: %q is not a valid value (allowed: public, read_only, write_only, hidden, internal)",
					tableName, colName, access,
				))
				continue
			}
			if excluded[colName] {
				*errs = append(*errs, fmt.Errorf(
					"tables.%s.column_map.%s.access: column %q is removed by exclude_columns — access has no surface to act on",
					tableName, colName, colName,
				))
			}
		}
	}
}

// validateColumnOverrides enforces the config-phase §8.5 rules on
// `tables.<t>.column_map.<col>`, for both fields that reshape the generated
// Go field: `.name` and `.type` / `.import`.
//
// `.name` must be a valid *exported* Go identifier. Exported is required
// because the generated struct fields the override renames must be reachable
// from consumer code and from database scanning. A PascalCase form of a Go
// keyword (Type, Range, Map) is a legal identifier and is deliberately
// accepted — the reserved-word escape is locals-only (PRD §8.5 "Reserved
// Word Handling").
//
// `.type` must parse as a Go type expression, and `.import` is meaningless
// without it — an import path with nothing to import for is a typo, not a
// no-op.
//
// Either field on a column that exclude_columns removes is an error: nothing
// survives to rename or retype. Column existence needs the parsed schema and
// lives in validateColumnOverridesSchema. Two rules need the type / naming
// engines and are enforced in the gen package, where the resolution actually
// happens: the resolved-name collision rule, and the rejection of a
// package-qualified type literal that could never be imported.
func validateColumnOverrides(cfg *RootConfig, errs *[]error) {
	for _, tableName := range sortedTableKeys(cfg.Tables) {
		table := cfg.Tables[tableName]
		validateTypeMapLiterals(tableName, table.TypeMap, errs)
		if len(table.ColumnMap) == 0 {
			continue
		}
		excluded := make(map[string]bool)
		for _, c := range ResolveTableExcludeColumns(table, cfg.Generation) {
			excluded[c] = true
		}
		for _, colName := range slices.Sorted(maps.Keys(table.ColumnMap)) {
			override := table.ColumnMap[colName]
			validateColumnNameOverride(tableName, colName, override.Name, excluded[colName], errs)
			validateColumnTypeOverride(tableName, colName, override, excluded[colName], errs)
		}
	}
}

// validateTypeMapLiterals applies the same syntax rule to `type_map` values
// that `column_map.<col>.type` gets: the literal is written verbatim into the
// generated struct, so a value that is not a type expression is a typo the
// consumer should hear about here rather than from the Go compiler.
//
// The import rule the two forms differ on is enforced in the gen package,
// which owns the registry that decides whether a literal needs one.
func validateTypeMapLiterals(tableName string, typeMap map[string]string, errs *[]error) {
	for _, colName := range slices.Sorted(maps.Keys(typeMap)) {
		literal := typeMap[colName]
		if literal == "" {
			*errs = append(*errs, fmt.Errorf(
				"tables.%s.type_map.%s: is empty — remove the entry or name a Go type",
				tableName, colName,
			))
			continue
		}
		if !isGoTypeExpression(literal) {
			*errs = append(*errs, fmt.Errorf(
				"tables.%s.type_map.%s: %q is not a Go type expression",
				tableName, colName, literal,
			))
		}
	}
}

// validateColumnNameOverride enforces the §8.5 rules on one column's `.name`.
func validateColumnNameOverride(tableName, colName, name string, excluded bool, errs *[]error) {
	if name == "" {
		return
	}
	if !token.IsIdentifier(name) {
		*errs = append(*errs, fmt.Errorf(
			"tables.%s.column_map.%s.name: %q is not a valid Go identifier",
			tableName, colName, name,
		))
		return
	}
	if !token.IsExported(name) {
		*errs = append(*errs, fmt.Errorf(
			"tables.%s.column_map.%s.name: %q must be exported — generated struct fields start with an upper-case letter",
			tableName, colName, name,
		))
		return
	}
	if excluded {
		*errs = append(*errs, fmt.Errorf(
			"tables.%s.column_map.%s.name: column %q is removed by exclude_columns — there is no field to rename",
			tableName, colName, colName,
		))
	}
}

// validateColumnTypeOverride enforces the §8.5 rules on one column's `.type`
// and `.import`. The pairing rule runs first: `.import` alone
// imports a package nothing references.
func validateColumnTypeOverride(tableName, colName string, override ColumnOverride, excluded bool, errs *[]error) {
	if override.Type == "" {
		if override.Import != "" {
			*errs = append(*errs, fmt.Errorf(
				"tables.%s.column_map.%s.import: %q is set without a type — an import path with nothing to import for",
				tableName, colName, override.Import,
			))
		}
		return
	}
	if !isGoTypeExpression(override.Type) {
		*errs = append(*errs, fmt.Errorf(
			"tables.%s.column_map.%s.type: %q is not a Go type expression",
			tableName, colName, override.Type,
		))
		return
	}
	if excluded {
		*errs = append(*errs, fmt.Errorf(
			"tables.%s.column_map.%s.type: column %q is removed by exclude_columns — there is no field to retype",
			tableName, colName, colName,
		))
	}
}

// isGoTypeExpression reports whether lit parses as a Go type expression —
// an identifier (`string`, `Address`), a qualified identifier
// (`decimal.Decimal`), a generic instantiation (`null.Value[uuid.UUID]`), or
// a composition of those (`*T`, `[]T`, `map[K]V`, `chan T`, an inline struct
// / interface / func type).
//
// This is a syntax check, not a resolution check: sqlgen never loads the
// consumer's packages, so whether the named type exists is the Go compiler's
// verdict on the generated file. Rejecting `12` or `not a type` here keeps a
// typo from reaching that far.
func isGoTypeExpression(lit string) bool {
	expr, err := parser.ParseExpr(lit)
	if err != nil {
		return false
	}
	return isTypeExpr(expr)
}

// isTypeExpr reports whether expr is shaped like a type rather than a value.
// Several Go productions are ambiguous out of context — `*x` is both a
// pointer type and a dereference, `f[T]` both an instantiation and an index —
// so the operands decide.
func isTypeExpr(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.Ident, *ast.StructType, *ast.InterfaceType, *ast.FuncType:
		return true
	case *ast.ParenExpr:
		return isTypeExpr(e.X)
	case *ast.StarExpr:
		return isTypeExpr(e.X)
	case *ast.SelectorExpr:
		// Qualified identifier — `pkg.Type`. The qualifier must be a bare
		// package name, which rules out `a.b.C` and `f().T`.
		_, ok := e.X.(*ast.Ident)
		return ok
	default:
		return isCompositeTypeExpr(expr)
	}
}

// isCompositeTypeExpr handles the type forms built out of other types —
// containers and generic instantiations — so each operand is checked in turn.
func isCompositeTypeExpr(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.ArrayType:
		// Len is a constant expression (`[4]byte`), not a type — only the
		// element type is checked.
		return isTypeExpr(e.Elt)
	case *ast.MapType:
		return isTypeExpr(e.Key) && isTypeExpr(e.Value)
	case *ast.ChanType:
		return isTypeExpr(e.Value)
	case *ast.IndexExpr:
		// Single type argument — `omittable.Value[string]`.
		return isTypeExpr(e.X) && isTypeExpr(e.Index)
	case *ast.IndexListExpr:
		// Several type arguments — `pkg.Pair[string, int]`.
		return isTypeExpr(e.X) && allTypeExprs(e.Indices)
	default:
		return false
	}
}

// allTypeExprs reports whether every expression in exprs is shaped like a type.
func allTypeExprs(exprs []ast.Expr) bool {
	for _, expr := range exprs {
		if !isTypeExpr(expr) {
			return false
		}
	}
	return true
}

// validateColumnOverridesSchema enforces the schema-phase §8.5 rule: a
// `column_map.<col>` entry that reshapes the generated field — `.name` or
// `.type` — must name a column that exists on the parsed table. Renaming or
// retyping a column that is not there is a silent no-op otherwise. Columns
// removed by exclude_columns are already reported pre-parse.
//
// Entries carrying only `.description` or `.access` are left alone: those
// have their own rules, and `.access` already runs this check in
// validateColumnAccessSchema.
func validateColumnOverridesSchema(cfg *RootConfig, tables []SchemaTable, errs *[]error) {
	for _, table := range tables {
		tc := findTableConfig(cfg, table)
		if tc == nil || len(tc.ColumnMap) == 0 {
			continue
		}
		cols := make(map[string]bool, len(table.Columns))
		for _, col := range table.Columns {
			cols[col.Name] = true
		}
		excluded := make(map[string]bool)
		for _, c := range ResolveTableExcludeColumns(*tc, cfg.Generation) {
			excluded[c] = true
		}
		for _, colName := range slices.Sorted(maps.Keys(tc.ColumnMap)) {
			if cols[colName] || excluded[colName] {
				continue
			}
			override := tc.ColumnMap[colName]
			for _, field := range [...]struct{ name, value string }{
				{"name", override.Name},
				{"type", override.Type},
			} {
				if field.value == "" {
					continue
				}
				*errs = append(*errs, fmt.Errorf(
					"tables.%s.column_map.%s.%s: column %q does not exist on the table",
					qualifiedTableName(table), colName, field.name, colName,
				))
			}
		}
	}
}

// validateRelationships enforces the §13.7.3 relationship dedup rules and the
// §4.8 distinct-name requirement on every table's `relationships:` block.
//
// PRD §13.7.3 dedup key:
//   - o2o, o2m: (target_table, fk_column, filter, discriminator)
//   - m2m:      (target_table, junction, junction_local_fk, junction_reference_fk, filter, discriminator)
//
// The `filter:` portion is compared byte-equal — no whitespace normalization
// or canonicalization (PRD §13.7.1). Two relationships with semantically
// identical filters written differently are accepted as distinct sub-categories.
// The `discriminator:` portion is its column and value, compared the same way.
//
// It rejects a missing or unknown `type:`, a missing o2o / o2m `fk:` or m2m
// `junction:`, and an unknown `side:` (validateRelationshipKeys).
//
// It also enforces the §4.13 mutual exclusion of `filter:` and
// `discriminator:` on one relationship: composed, their write-side inverse
// would be undefined.
//
// Distinct-name check is per-table: two relationships on the same parent must
// not share `name:`, because each name maps to a field on `<Table>FieldOptions`
// and a Go field collision would surface as a compile error far from the
// config typo.
func validateRelationships(cfg *RootConfig, errs *[]error) {
	for _, tableName := range sortedTableKeys(cfg.Tables) {
		table := cfg.Tables[tableName]
		if len(table.Relationships) == 0 {
			continue
		}
		seenName := make(map[string]int)
		seenKey := make(map[string]int)
		for i, r := range table.Relationships {
			if r.Name != "" {
				if prev, ok := seenName[r.Name]; ok {
					*errs = append(*errs, fmt.Errorf(
						"tables.%s.relationships[%d].name: duplicate relationship name %q (also at relationships[%d]); names must be distinct on a parent table (PRD §4.8)",
						tableName, i, r.Name, prev,
					))
				} else {
					seenName[r.Name] = i
				}
			}
			validateRelationshipKeys(tableName, i, r, errs)
			if r.Filter != "" && r.Discriminator != nil {
				*errs = append(*errs, fmt.Errorf(
					"tables.%s.relationships[%d] (%q): `filter` and `discriminator` are mutually exclusive on one relationship (PRD §4.13 / §13.4.1); `discriminator` is the invertible form and the only one nested mutations can write — drop `filter` to keep it",
					tableName, i, r.Name,
				))
			}
			validateRelationshipSort(tableName, i, r, errs)
			if cfg.Input.Dialect == DialectMySQL && filterHasBareDoubleQuote(r.Filter) {
				*errs = append(*errs, fmt.Errorf(
					"tables.%s.relationships[%d] (%q): filter %q contains a double-quoted token, which MySQL reads as a string literal rather than an identifier (PRD §13.7.1) — the predicate then compares two constants and silently matches no rows; write `col` to quote an identifier, or 'value' for a string",
					tableName, i, r.Name, r.Filter,
				))
			}
			key := RelationshipDedupKey(r)
			if key == "" {
				continue
			}
			if prev, ok := seenKey[key]; ok {
				*errs = append(*errs, fmt.Errorf(
					"tables.%s.relationships[%d]: duplicate relationship — same target, fk, filter and discriminator as relationships[%d] (PRD §13.7.3); give each entry a distinct filter or discriminator value to produce sub-categorized fields",
					tableName, i, prev,
				))
			} else {
				seenKey[key] = i
			}
		}
	}
}

// validateRelationshipKeys checks the keys PRD §4.8 constrains on their own:
//
//   - `type:` is required and one of the six spellings the generator maps; it
//     would otherwise be read as o2m.
//   - `fk:` is required on an o2o or o2m edge, and `junction:` on an m2m one.
//     Without `fk` the generator emits an o2m filter field on an empty column
//     name, which does not parse, and an o2o JOIN on an empty identifier,
//     which fails on the edge's first load; without `junction` an m2m edge
//     emits a duplicate empty switch case and the package does not compile.
//     A missing or invalid type reports the type error instead, since both
//     rules depend on it.
//   - `side:` is parent, child or empty.
func validateRelationshipKeys(tableName string, i int, r TableRelationship, errs *[]error) {
	switch {
	case r.Type == "":
		*errs = append(*errs, fmt.Errorf(
			"tables.%s.relationships[%d].type: required (allowed: o2o, one_to_one, o2m, one_to_many, m2m, many_to_many) (PRD §4.8)",
			tableName, i,
		))
	case !isValidRelationshipType(r.Type):
		*errs = append(*errs, fmt.Errorf(
			"tables.%s.relationships[%d].type: %q is not a valid value (allowed: o2o, one_to_one, o2m, one_to_many, m2m, many_to_many) (PRD §4.8)",
			tableName, i, r.Type,
		))
	case r.FK == "" && !isM2M(r.Type):
		*errs = append(*errs, fmt.Errorf(
			"tables.%s.relationships[%d] (%q).fk: required on an o2o or o2m relationship — it names the FK column that creates the link (PRD §4.8 / §4.13)",
			tableName, i, r.Name,
		))
	case r.Junction == "" && isM2M(r.Type):
		*errs = append(*errs, fmt.Errorf(
			"tables.%s.relationships[%d] (%q).junction: required on an m2m relationship — it names the junction table that links the two (PRD §4.8 / §4.13)",
			tableName, i, r.Name,
		))
	}
	if !isValidRelationshipSide(r.Side) {
		*errs = append(*errs, fmt.Errorf(
			"tables.%s.relationships[%d].side: %q is not a valid value (allowed: parent, child; default: parent) (PRD §4.8)",
			tableName, i, r.Side,
		))
	}
}

// validateRelationshipSort enforces the shape of a relationship's static
// `sort:` (PRD §4.8, §4.13). Each rule turns a config the generator would
// otherwise drop or defer into a runtime SQL error into a validate-time one:
//
//   - An o2o edge is LEFT JOINed into the parent query (§13.2) and has no
//     loader query of its own to order, so `sort:` there would be ignored.
//   - `column` is required; an empty one would render `ORDER BY ""`.
//   - `direction` is the closed set schema/v1.json declares.
//
// That the column exists on the related table is a schema-level check
// (validateRelationshipSortColumns).
func validateRelationshipSort(tableName string, i int, r TableRelationship, errs *[]error) {
	if len(r.Sort) == 0 {
		return
	}
	if isO2O(r.Type) {
		*errs = append(*errs, fmt.Errorf(
			"tables.%s.relationships[%d] (%q).sort: an o2o relationship is loaded by a LEFT JOIN in the parent query and has no loader query to order, so a static sort would be ignored (PRD §4.8 / §13.2) — drop `sort`, and order the parent query with its own Sorts instead",
			tableName, i, r.Name,
		))
		return
	}
	for j, s := range r.Sort {
		if s.Column == "" {
			*errs = append(*errs, fmt.Errorf(
				"tables.%s.relationships[%d] (%q).sort[%d].column: required — it names the column on the related table to order by (PRD §4.8)",
				tableName, i, r.Name, j,
			))
		}
		switch s.Direction {
		case "", "asc", "desc", "ASC", "DESC":
		default:
			*errs = append(*errs, fmt.Errorf(
				"tables.%s.relationships[%d] (%q).sort[%d].direction: %q is not a valid value (allowed: asc, desc; default: asc) (PRD §4.8)",
				tableName, i, r.Name, j, s.Direction,
			))
		}
	}
}

// validateRelationshipSortColumns enforces that every static `sort:` column
// names a real column on the *related* table (PRD §4.8, §4.13). The loader
// passes the sort to the related table's query, so an unknown column would
// fail every relationship load at runtime rather than at validate time.
//
// As with validateRelationshipDiscriminators, a relationship whose target is
// absent from the parsed tables is skipped: the generator's relationship
// resolution reports an unknown target, and a second error here would name the
// sort for a problem the sort does not have. A view target is skipped the same
// way (PRD §4.13); its bad sort column fails as an SQL error on first load. An
// o2o edge is skipped too — pre-parse validation already rejects its `sort:`
// outright.
func validateRelationshipSortColumns(cfg *RootConfig, tables []SchemaTable, errs *[]error) {
	var parsed map[string][]SchemaColumn

	for _, tableName := range sortedTableKeys(cfg.Tables) {
		for i, r := range cfg.Tables[tableName].Relationships {
			if len(r.Sort) == 0 || isO2O(r.Type) {
				continue
			}
			if parsed == nil {
				parsed = make(map[string][]SchemaColumn, len(tables)*2)
				for _, t := range tables {
					parsed[t.Name] = t.Columns
					parsed[qualifiedTableName(t)] = t.Columns
				}
			}
			cols, ok := parsed[r.Table]
			if !ok {
				continue
			}
			for j, s := range r.Sort {
				if s.Column == "" || slices.ContainsFunc(cols, func(c SchemaColumn) bool { return c.Name == s.Column }) {
					continue
				}
				*errs = append(*errs, fmt.Errorf(
					"tables.%s.relationships[%d] (%q).sort[%d].column: %q is not a column on related table %q (PRD §4.8 / §4.13)",
					tableName, i, r.Name, j, s.Column, r.Table,
				))
			}
		}
	}
}

// validateRelationshipFKColumns enforces PRD §4.8's `fk` row against the parsed
// tables. An o2m `fk` is a column on the related table. An o2o `fk` is a column
// on this table (belongs-to) or on the related one (has-one), and §13.1 derives
// which from where it is declared, so it must be on one of them. The generator
// reads a column found on neither side as belongs-to (fkColumnOnTarget): an
// o2m edge then emits a filter field the related type lacks and fails in the
// Go compile, and an o2o edge emits a JOIN on a column that does not exist and
// fails on its first load.
//
// The type arms mirror the generator's parseRelationshipType, which reads any
// spelling other than o2o and m2m as o2m. validateRelationships rejects any
// other spelling before parsing, so in practice the o2m arm sees
// only o2m and one_to_many. An m2m edge links through its
// junction's own fields and is skipped, as is an empty `fk`, which
// validateRelationships rejects before parsing. A target absent
// from the parsed tables is skipped as the sibling checks skip it: the
// generator's relationship resolution reports it, and a view target is left to
// that resolution too (PRD §4.13).
func validateRelationshipFKColumns(cfg *RootConfig, tables []SchemaTable, errs *[]error) {
	var parsed map[string][]SchemaColumn

	hasColumn := func(cols []SchemaColumn, name string) bool {
		return slices.ContainsFunc(cols, func(c SchemaColumn) bool { return c.Name == name })
	}

	for _, tableName := range sortedTableKeys(cfg.Tables) {
		for i, r := range cfg.Tables[tableName].Relationships {
			if r.FK == "" || isM2M(r.Type) {
				continue
			}
			if parsed == nil {
				parsed = make(map[string][]SchemaColumn, len(tables)*2)
				for _, t := range tables {
					parsed[t.Name] = t.Columns
					parsed[qualifiedTableName(t)] = t.Columns
				}
			}
			targetCols, ok := parsed[r.Table]
			if !ok || hasColumn(targetCols, r.FK) {
				continue
			}
			if !isO2O(r.Type) {
				*errs = append(*errs, fmt.Errorf(
					"tables.%s.relationships[%d] (%q).fk: %q is not a column on related table %q (PRD §4.8)",
					tableName, i, r.Name, r.FK, r.Table,
				))
				continue
			}
			sourceCols, ok := parsed[tableName]
			if !ok || hasColumn(sourceCols, r.FK) {
				continue
			}
			*errs = append(*errs, fmt.Errorf(
				"tables.%s.relationships[%d] (%q).fk: %q is not a column on table %q or on related table %q; an o2o fk is on one of them (PRD §4.8 / §13.1)",
				tableName, i, r.Name, r.FK, tableName, r.Table,
			))
		}
	}
}

// RelationshipDedupKey returns the §13.7.3 dedup key for a single relationship.
// Empty `Table` means the entry is malformed (a different validator surfaces
// that); we return "" so dedup is skipped without false-positive collisions.
//
// The key compares `table:` as written, which is all this pre-parse pass can
// see: `documents` and `public.documents` produce different keys here. The
// generator re-runs the check once each target has resolved, keying on the
// resolved `schema.table` (gen.mergeDeclaredRelationships), which is
// why the function is exported.
//
// Per PRD §13.7.3 the key bundles o2o and o2m under a single shape — the
// relationship type is intentionally *not* part of the key so a config that
// declares the same `(target, fk, filter)` once as o2o and once as o2m is
// flagged as a duplicate. This also subsumes the parser's synonym handling
// (`one_to_one`↔`o2o`, `one_to_many`↔`o2m`, `many_to_many`↔`m2m` from
// `parseRelationshipType`); since type isn't keyed, synonym variants can't
// silently bypass dedup. M2M is structurally distinguished by its longer
// key (junction fields), prefixed with a sentinel so an o2o/o2m entry with
// empty fk can never alias into an m2m bucket.
//
// The key uses `\x00` as a field separator — a byte that cannot appear in a
// valid SQL identifier or filter string, so distinct fields never alias into
// the same key.
//
// The key carries BOTH discriminator forms (PRD §13.7.1): `filter` and the
// `discriminator`'s column and value. Sub-categorized edges share their
// (target, fk) prefix by definition — that is what makes them
// sub-categorized — so the discriminating value *is* the key. Were it to carry
// `filter` alone, three `discriminator:` edges into one child table on one FK
// would collapse onto a single key and be rejected as duplicates, which would
// make the structured form unusable for exactly the pattern it exists to
// describe. The two forms are mutually exclusive, so at most one contributes.
func RelationshipDedupKey(r TableRelationship) string {
	if r.Table == "" {
		return ""
	}
	const sep = "\x00"
	disc := discriminatorDedupComponent(r.Discriminator)
	if isM2M(r.Type) {
		return "m" + sep + r.Table + sep + r.Junction + sep + r.JunctionLocalFK + sep + r.JunctionReferenceFK + sep + r.Filter + sep + disc
	}
	return "x" + sep + r.Table + sep + r.FK + sep + r.Filter + sep + disc
}

// filterHasBareDoubleQuote reports whether a `filter:` predicate contains a
// double quote outside a string literal or a backtick-quoted identifier.
//
// On MySQL a double-quoted token is a *string literal*, not a quoted
// identifier: vitess parses `"entity_type" = 'asset.primary'` to
// `'entity_type' = 'asset.primary'`, a comparison of two constants that is
// false for every row and raises no warning (a string-vs-number form raises
// only warning 1292). The relationship then silently loads nothing.
//
// Setting ANSI_QUOTES does not rescue it. The o2m/m2m loader hands the filter
// text to MySQL verbatim, so that path starts working; the o2o JOIN ON and the
// relationship-filter EXISTS are qualified by vitess at *generation* time, so
// the literal is already baked into the generated source before MySQL sees it
// and stays false. One `filter:` would then mean two different things on two
// read paths, which is why this is rejected rather than documented.
//
// A double quote inside a string literal ('say "hi"') or inside a
// backtick-quoted identifier is ordinary MySQL and is left alone, so the scan
// tracks both. Backslash escapes are honoured because MySQL applies them
// inside literals unless NO_BACKSLASH_ESCAPES is set; treating `\'` as a close
// quote would misread the rest of the predicate.
func filterHasBareDoubleQuote(filter string) bool {
	var inLiteral, inIdent bool
	for i := 0; i < len(filter); i++ {
		switch filter[i] {
		case '\\':
			if inLiteral {
				i++ // the escaped byte cannot close the literal
			}
		case '\'':
			if inIdent {
				continue
			}
			if inLiteral && i+1 < len(filter) && filter[i+1] == '\'' {
				i++ // a doubled quote stays inside the literal
				continue
			}
			inLiteral = !inLiteral
		case '`':
			if !inLiteral {
				inIdent = !inIdent
			}
		case '"':
			if !inLiteral && !inIdent {
				return true
			}
		}
	}
	return false
}

// discriminatorDedupComponent renders a discriminator as its column and value
// in that order (PRD §13.7.1), compared byte-equal like the `filter` portion.
// A nil discriminator contributes two empty fields rather than none, so the
// component count is fixed and a `filter:` edge can never alias onto a
// `discriminator:` one.
//
// The value's Go type is part of the rendering: `value` is a YAML scalar, not
// a string (PRD §13.4.1), so `1` and `"1"` are different declarations and must
// not collapse onto one key.
func discriminatorDedupComponent(d *RelationshipDiscriminator) string {
	const sep = "\x00"
	if d == nil {
		return sep
	}
	return d.Column + sep + fmt.Sprintf("%T:%v", d.Value, d.Value)
}

// isM2M reports whether a relationship type spelling refers to many-to-many.
// Mirrors the synonyms accepted by `parseRelationshipType` in
// `cmd/sqlgen/gen/context_table.go` so validation and codegen agree on what
// counts as an m2m relationship for dedup-shape purposes.
func isM2M(t string) bool {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "m2m", "many_to_many":
		return true
	}
	return false
}

// isValidRelationshipType reports whether a `type:` YAML value is one of the
// six spellings `parseRelationshipType` (cmd/sqlgen/gen/context_table.go)
// maps. That function returns one-to-many for anything else, so without this
// gate a typo like `one-to-one` generated an O2M edge with no diagnostic.
// It folds case exactly as `parseRelationshipType` does and, unlike isM2M,
// does not trim: `parseRelationshipType` would read " o2o" as one-to-many, so
// the padded spelling is rejected here rather than accepted and mis-generated.
func isValidRelationshipType(t string) bool {
	switch strings.ToLower(t) {
	case "o2o", "one_to_one", "o2m", "one_to_many", "m2m", "many_to_many":
		return true
	}
	return false
}

// isValidRelationshipSide reports whether a `side:` YAML value is recognized
// by `parseRelationshipSide` (cmd/sqlgen/gen/context_table.go). Empty
// resolves to "parent" (default); `parent` / `child` are explicit. Anything
// else silently defaulted to "parent" without this gate, so a typo like
// `side: chld` could flip an inverse-side entry's manifest kind from `m2o`
// back to `o2o` without diagnosis.
func isValidRelationshipSide(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "parent", "child":
		return true
	}
	return false
}

// sortedTableKeys returns the keys of cfg.Tables sorted alphabetically so the
// per-table relationship validation iterates deterministically (Go map order
// is randomized). Without this, the diagnostic order of multi-table errors
// would be unstable across runs and break test pinning.
func sortedTableKeys(tables map[string]TableConfig) []string {
	keys := make([]string, 0, len(tables))
	for k := range tables {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedViewKeys is sortedTableKeys' twin for cfg.Views, for the same reason:
// a config with several offending views must report them in a stable order.
func sortedViewKeys(views map[string]ViewConfig) []string {
	keys := make([]string, 0, len(views))
	for k := range views {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// validateAPIConfig validates the api: block. It rejects unknown
// field_casing values and any custom scalar that lacks a `marshaling:`
// declaration (per PRD §26.4.1 — sqlgen cannot emit a non-compiling
// resolver for an unknown type without a declared marshaling mode).
//
// Also rejects an output.package value that collides with the
// sqlgen-controlled `gqlmodel` import alias used to qualify gqlgen-emitted
// input types in seed bodies and per-table translators. The alias is never
// emitted with api.graphql disabled, so — like every other api.graphql.*
// field — the whole block is skipped in that case.
//
// §26.5.8: when api.graphql.enabled is false the entire api.graphql block is
// inert — no field is validated, resolved, or emitted. The early return keeps
// disabled configs from erroring on fields (field_casing, scalars,
// output.package collision) that never surface in generated output.
func validateAPIConfig(cfg *RootConfig, errs *[]error) {
	if cfg.API == nil || cfg.API.GraphQL == nil {
		return
	}
	g := cfg.API.GraphQL
	// Mirror BuildAPIContext's emission gate exactly: GraphQL is active (and its
	// fields matter) only when both api.enabled and api.graphql.enabled are true.
	// Keying on both keeps validation and emission from disagreeing — a top-level
	// api.enabled:false disables the whole subtree, graphql included.
	if !cfg.API.Enabled || !g.Enabled {
		return
	}
	if g.FieldCasing != "" && !IsValidFieldCasing(g.FieldCasing) {
		*errs = append(*errs, fmt.Errorf("api.graphql.field_casing: %q is not a valid value (allowed: camel_case, snake_case)", g.FieldCasing))
	}
	for _, name := range slices.Sorted(maps.Keys(g.Scalars)) {
		validateScalarBinding(name, g.Scalars[name], errs)
	}
	validateScalarGoTypeUniqueness(g.Scalars, errs)
	if cfg.Output.Package == GqlgenModelImportAlias {
		*errs = append(*errs, fmt.Errorf("output.package: %q collides with the sqlgen-controlled gqlgen import alias %q used in generated graph-package files; rename output.package", cfg.Output.Package, GqlgenModelImportAlias))
	}
}

// validateScalarGoTypeUniqueness rejects two `api.graphql.scalars` entries
// that declare the same `go_type`.
//
// The generator's lookup is keyed by Go type, so a Go type resolves to exactly
// one GraphQL scalar: whichever entry wins, the other is never consulted, never
// declared in the schema, and never merged into gqlgen.yml. There is no way to
// express "this column gets scalar A and that one gets scalar B" for a single
// Go type, so a duplicate is always a mistake — and a silent one, which is the
// declared-but-unread shape §26.4.1's rules exist to prevent.
//
// Reported against the second name in sorted order so the message is stable
// across runs.
func validateScalarGoTypeUniqueness(scalars map[string]ScalarBinding, errs *[]error) {
	declaredBy := make(map[string]string, len(scalars))
	for _, name := range slices.Sorted(maps.Keys(scalars)) {
		goType := scalars[name].GoType
		if goType == "" {
			continue
		}
		if first, dup := declaredBy[goType]; dup {
			*errs = append(*errs, fmt.Errorf("api.graphql.scalars.%s.go_type: %q is already declared by api.graphql.scalars.%s; a Go type resolves to exactly one GraphQL scalar, so this entry would be silently ignored", name, goType, first))
			continue
		}
		declaredBy[goType] = name
	}
}

// validateScalarBinding enforces the §26.4.1 rules for one
// `api.graphql.scalars` entry.
//
// The map declares scalars sqlgen does not own the marshaling for: sqlgen
// wires the schema declaration and the gqlgen `models:` binding, and the
// consumer supplies the marshaling. What "supplies" means is what
// `marshaling:` selects, and each mode carries a different obligation:
//
//   - builtin  — gqlgen ships the marshaler (Time, Map, Upload, Any, Int64),
//     and sqlgen pins the `models:` entry to it. Both halves of that are
//     checked: the name has to be one gqlgen actually bundles, and the
//     `go_type` one that marshaler carries.
//   - method   — the Go type itself carries MarshalGQL / UnmarshalGQL, so the
//     binding points straight at the type and needs its full import path.
//   - external — free `Marshal<Name>` / `Unmarshal<Name>` functions live in
//     `marshaler_package`, which is bound as gqlgen's discovery anchor.
//
// A `go_type` the built-in registry already owns is rejected outright; see
// BuiltInScalarGoTypes for why an override cannot be honored.
func validateScalarBinding(name string, sc ScalarBinding, errs *[]error) {
	if !IsValidGraphQLName(name) {
		*errs = append(*errs, fmt.Errorf("api.graphql.scalars.%s: %q is not a valid GraphQL name; the key is emitted verbatim as `scalar <name>` in the generated schema", name, name))
	}
	if reason, ok := ReservedScalarNames[name]; ok {
		*errs = append(*errs, fmt.Errorf("api.graphql.scalars.%s: %q is %s and cannot be redeclared (§26.4.1) — sqlgen keys the Null-wrapper pairing, the comparator narrowing and scalar registration on the name, so a redeclaration rebinds the built-in rather than adding to it; choose a different scalar name", name, name, reason))
	}
	if sc.Marshaling == "" {
		*errs = append(*errs, fmt.Errorf("api.graphql.scalars.%s: marshaling is required (allowed: builtin, method, external)", name))
		return
	}
	if !IsValidScalarMarshaling(sc.Marshaling) {
		*errs = append(*errs, fmt.Errorf("api.graphql.scalars.%s.marshaling: %q is not a valid value (allowed: builtin, method, external)", name, sc.Marshaling))
	}
	if sc.Marshaling == ScalarMarshalingExternal && sc.MarshalerPackage == "" {
		*errs = append(*errs, fmt.Errorf("api.graphql.scalars.%s.marshaler_package: required when marshaling is %q (the import path of the package declaring Marshal%s / Unmarshal%s)", name, sc.Marshaling, name, name))
	}
	if sc.Marshaling != ScalarMarshalingExternal && sc.MarshalerPackage != "" {
		*errs = append(*errs, fmt.Errorf("api.graphql.scalars.%s.marshaler_package: only valid when marshaling is %q (got marshaling %q)", name, ScalarMarshalingExternal, sc.Marshaling))
	}
	if sc.GoType == "" {
		if sc.Marshaling != ScalarMarshalingBuiltin {
			*errs = append(*errs, fmt.Errorf("api.graphql.scalars.%s.go_type: required when marshaling is %q", name, sc.Marshaling))
		}
		return
	}
	importPath, typeName := SplitGoType(sc.GoType)
	if sc.Marshaling == ScalarMarshalingMethod && importPath == "" {
		*errs = append(*errs, fmt.Errorf("api.graphql.scalars.%s.go_type: %q has no package qualifier; marshaling %q needs the full import path (e.g. %q)", name, sc.GoType, sc.Marshaling, "example.com/types.Email"))
	}
	// Compared on the qualified short form ("decimal.Decimal"), which is how
	// both the registry and a resolved column spell a Go type. Any package
	// whose last element and type name match a registry entry therefore
	// collides — correctly so, because gen's lookup is keyed the same way and
	// would route the column to the registry scalar regardless.
	qualified := QualifiedGoType(importPath, typeName)
	if builtIn, ok := BuiltInScalarGoTypes[qualified]; ok {
		*errs = append(*errs, fmt.Errorf("api.graphql.scalars.%s.go_type: %q resolves to %q, which is owned by the built-in scalar %q; the built-in registry is authoritative for this Go type (§26.4.1) — remove this entry, or declare the scalar on a Go type the registry does not cover", name, sc.GoType, qualified, builtIn))
	}
	validateBuiltinScalarBinding(name, sc, qualified, errs)
}

// validateBuiltinScalarBinding enforces the two preconditions `marshaling:
// builtin` carries, both of which sqlgen used to take on faith.
//
// The mode delegates marshaling to a function gqlgen itself ships, so the
// declaration is only meaningful if there IS one: the scalar name has to be
// one gqlgen bundles, and the `go_type` one that bundled marshaler carries.
// Neither failure announces itself downstream. gqlgen leaves a scalar it does
// not recognise unbound and silently binds it to `string`; and it binds a
// bundled scalar to its OWN Go type, not the column's. Either way the
// generated input field faces a model field it cannot be assigned to and the
// read side degrades to a `panic("not implemented")` field resolver — an
// unbound-scalar failure arriving as a Go type error in generated code at the
// far end of the pipeline rather than as a diagnostic naming the entry.
//
// Only reached once `go_type` is known, because an entry without one binds
// nothing to check: buildConsumerScalarIndex skips it, so it reaches no
// column, declares no `scalar`, and emits no `models:` entry. Note this is the
// one shape that is genuinely silent — unusedScalarWarnings skips an empty
// `go_type` too, so a typo'd bare `builtin` entry is inert rather than
// diagnosed. Inert is why it is left alone, not a warning covering it.
func validateBuiltinScalarBinding(name string, sc ScalarBinding, qualified string, errs *[]error) {
	if sc.Marshaling != ScalarMarshalingBuiltin {
		return
	}
	if !GqlgenBundlesScalar(name) {
		*errs = append(*errs, fmt.Errorf("api.graphql.scalars.%s.marshaling: %q names a marshaler gqlgen ships, but gqlgen bundles none for %q (it bundles %s); gqlgen leaves an unrecognised scalar unbound and silently binds it to `string`, which yields a non-compiling input translator and a panic(\"not implemented\") field resolver — use marshaling %q or %q, which declare where the marshaler lives (§26.4.1)", name, ScalarMarshalingBuiltin, name, strings.Join(GqlgenBundledScalarNames(), ", "), ScalarMarshalingMethod, ScalarMarshalingExternal))
		return
	}
	if GqlgenBundledMarshalerPath(name, qualified) == "" {
		*errs = append(*errs, fmt.Errorf("api.graphql.scalars.%s.go_type: %q resolves to %q, which gqlgen's bundled %s marshaler does not carry (it carries %s); gqlgen would bind the scalar to its own Go type and the generated input field would not be assignable to the model — use marshaling %q or %q, or declare the scalar on a Go type the bundled marshaler covers (§26.4.1)", name, sc.GoType, qualified, name, strings.Join(GqlgenBundledGoTypes(name), " / "), ScalarMarshalingMethod, ScalarMarshalingExternal))
	}
}

// validateAPIOperations enforces the config-level §26.5.1 rules for the
// `api.operations` mask, globally and per table:
//
//   - The preset, if any, must be a recognized name.
//   - Client-only keys (get_many, exists, count, increment, stream,
//     update_many, upsert_many) name client methods no API surface calls, so
//     setting one explicitly is an error rather than a silently-ignored knob.
//
// The third rule, the over-reach warning on a per-table key the schema does
// not allow, needs each table's resolved operations and so runs at resolution
// level (gen.apiOverReachWarnings, PRD §4.13).
func validateAPIOperations(cfg *RootConfig, errs *[]error) {
	if cfg.API == nil {
		return
	}
	if cfg.API.Operations != nil {
		checkAPIOperationsBlock("api.operations", *cfg.API.Operations, errs)
	}
	for _, tableName := range sortedTableKeys(cfg.Tables) {
		table := cfg.Tables[tableName]
		if table.API == nil || table.API.Operations == nil {
			continue
		}
		checkAPIOperationsBlock(fmt.Sprintf("tables.%s.api.operations", tableName), *table.API.Operations, errs)
	}
}

// validateViewAPIOperations enforces the read-only rule on every
// `views.<name>.api.operations` block (PRD §4.13, §26.4 "Views on the GraphQL
// surface").
//
// A view has no mutation half, so naming a mutation there is rejected rather
// than silently dropped — the same call clientOnlyOperationFields already makes
// for `get_many` / `exists` / `count` / `increment` / `stream` /
// `update_many` / `upsert_many`, one entity kind over. Both lists are checked, because both
// name operations the block cannot express; only the reason differs, and the
// message says which.
func validateViewAPIOperations(cfg *RootConfig, errs *[]error) {
	for _, viewName := range sortedViewKeys(cfg.Views) {
		view := cfg.Views[viewName]
		if view.API == nil || view.API.Operations == nil {
			continue
		}
		mask := *view.API.Operations
		path := fmt.Sprintf("views.%s.api.operations", viewName)

		// The preset and client-only halves are the same rules a table's block
		// answers, so they are answered by the same function rather than a
		// second copy that could word them differently.
		checkAPIOperationsBlock(path, mask, errs)

		for _, f := range mutationAPIOperationFields {
			if *f.Field(&mask) == nil {
				continue
			}
			*errs = append(*errs, fmt.Errorf(
				"%s.%s: %q is a mutation and view %q is read-only — only get, paginate, connection are expressible here; remove it (PRD §16.4 / §26.4)",
				path, f.Name, f.Name, viewName,
			))
		}
	}
}

// validateViewAPIOptIn rejects the opt-in form of the per-view API flag. Like
// tables.<name>.manifest.enabled, the per-view flag is opt-OUT only: a single
// entity cannot be opted in without the package-level opt-in, because the
// generator emits no GraphQL surface at all when `api.enabled` is false
// (PRD §4.13).
func validateViewAPIOptIn(cfg *RootConfig, errs *[]error) {
	globalEnabled := cfg.API != nil && cfg.API.Enabled
	for _, name := range sortedViewKeys(cfg.Views) {
		v := cfg.Views[name]
		if v.API == nil || v.API.Enabled == nil {
			continue
		}
		if *v.API.Enabled && !globalEnabled {
			*errs = append(*errs, fmt.Errorf(
				"views.%s.api.enabled: cannot be true when global api.enabled is false — the per-view flag is opt-out only (PRD §4.13 / §26.4)",
				name,
			))
		}
	}
}

// retiredAPIKeyHint names the key that gates the surface a user most likely
// meant, for the two client-only keys that once gated an API surface.
var retiredAPIKeyHint = map[string]string{
	"get_many":    " (<table>List is backed by Paginate and gated on paginate)",
	"update_many": " (update<Table>s is backed by UpdateWhere and gated on update_where)",
}

// checkAPIOperationsBlock validates one `api.operations` block in isolation:
// the preset name, and the absence of client-only operations.
func checkAPIOperationsBlock(path string, mask Operations, errs *[]error) {
	if mask.Preset != "" && !IsValidPreset(mask.Preset) {
		*errs = append(*errs, fmt.Errorf(
			"%s.preset: %q is not a valid value (allowed: all, read_only, append_only, no_delete, no_hard_delete)",
			path, mask.Preset,
		))
	}
	for _, f := range clientOnlyOperationFields {
		if *f.Field(&mask) == nil {
			continue
		}
		*errs = append(*errs, fmt.Errorf(
			"%s.%s: %q is not an API operation — it names a client method no API surface calls, and the Go client generates it whenever the schema allows; remove it%s (PRD §26.5.1)",
			path, f.Name, f.Name, retiredAPIKeyHint[f.Name],
		))
	}
}

// GqlgenModelImportAlias is the sqlgen-controlled import alias that the
// generated graph-package files use to qualify gqlgen-emitted input types
// (Create<T>Input, <T>FilterInput, scalar comparator inputs, …). Mirrored
// in cmd/sqlgen/gen so the alias spelling is the same on both sides;
// validation here guards against an output.package collision.
const GqlgenModelImportAlias = "gqlmodel"

// enumValidator captures a single "is this enum value allowed?" check.
type enumValidator struct {
	path    string
	value   string
	valid   bool
	allowed string
}

// validateEnumValues checks that every typed enum field in the config holds
// one of its defined constants. Empty values are skipped because applyDefaults
// fills them in during LoadConfig — tests may construct configs directly, and
// production flows always run through defaults before validation.
func validateEnumValues(cfg *RootConfig, errs *[]error) {
	checks := []enumValidator{
		{"input.dialect", string(cfg.Input.Dialect), cfg.Input.Dialect.IsValid(), "postgres, mysql, sqlite"},
		{"input.source", string(cfg.Input.Source), cfg.Input.Source.IsValid(), "files, database, both"},
		{"input.parse_mode", string(cfg.Input.ParseMode), cfg.Input.ParseMode.IsValid(), "strict, merge"},
		{"output.driver", string(cfg.Output.Driver), cfg.Output.Driver.IsValid(), "pgx, stdlib"},
		{"output.layout", string(cfg.Output.Layout), cfg.Output.Layout.IsValid(), "single_file, file_per_table"},
		{"generation.uuid_version", string(cfg.Generation.UUIDVersion), cfg.Generation.UUIDVersion.IsValid(), "v4, v7"},
	}
	for _, c := range checks {
		if c.value != "" && !c.valid {
			*errs = append(*errs, fmt.Errorf("%s: %q is not a valid value (allowed: %s)", c.path, c.value, c.allowed))
		}
	}
	validateSoftDeleteEnumTypes(cfg, errs)
	validateTablePrimaryKeyEnums(cfg, errs)
}

// validateSoftDeleteEnumTypes reports invalid entries in
// generation.soft_delete_columns[*].type.
func validateSoftDeleteEnumTypes(cfg *RootConfig, errs *[]error) {
	for i, sd := range cfg.Generation.SoftDeleteColumns {
		if sd.Type != "" && !sd.Type.IsValid() {
			*errs = append(*errs, fmt.Errorf("generation.soft_delete_columns[%d].type: %q is not a valid soft delete type (allowed: timestamp, bool, integer)", i, sd.Type))
		}
	}
}

// validateTablePrimaryKeyEnums reports invalid tables.*.primary_key enum values.
func validateTablePrimaryKeyEnums(cfg *RootConfig, errs *[]error) {
	for name, table := range cfg.Tables {
		if table.PrimaryKey == nil {
			continue
		}
		if s := table.PrimaryKey.Strategy; s != "" && !s.IsValid() {
			*errs = append(*errs, fmt.Errorf("tables.%s.primary_key.strategy: %q is not a valid strategy (allowed: db, app, caller)", name, s))
		}
		if v := table.PrimaryKey.UUIDVersion; v != "" && !v.IsValid() {
			*errs = append(*errs, fmt.Errorf("tables.%s.primary_key.uuid_version: %q is not a valid UUID version (allowed: v4, v7)", name, v))
		}
	}
}

// validateDriverDialect checks that pgx is only used with postgres.
func validateDriverDialect(cfg *RootConfig, errs *[]error) {
	if cfg.Output.Driver == DriverPgx && cfg.Input.Dialect != DialectPostgres {
		*errs = append(*errs, fmt.Errorf("output.driver: pgx is only compatible with input.dialect: postgres (got %q)", cfg.Input.Dialect))
	}
}

// validateConnectionRequired checks that connection is present when source requires it.
func validateConnectionRequired(cfg *RootConfig, errs *[]error) {
	needsConnection := cfg.Input.Source == SourceDatabase || cfg.Input.Source == SourceBoth
	if needsConnection && cfg.Input.Connection == nil {
		*errs = append(*errs, fmt.Errorf("input.connection is required when input.source is %q", cfg.Input.Source))
	}
}

// validateRemovedOperations rejects a leftover client `operations` key under
// `generation` or `tables.<name>` (PRD §4.13). The Go client generates every
// method the schema allows (§4.6), so the key has nothing to control; the
// message names the path and points at `api.operations`, the only
// per-operation control, which the strict decoder's unknown-field error would
// not.
func validateRemovedOperations(cfg *RootConfig, errs *[]error) {
	const msg = "%s: the Go client generates every method the schema allows; use api.operations to control what the API exposes (PRD §4.6 / §26.5.1)"
	if cfg.Generation.RemovedOperations.Kind != 0 {
		*errs = append(*errs, fmt.Errorf(msg, "generation.operations"))
	}
	for _, name := range sortedTableKeys(cfg.Tables) {
		if cfg.Tables[name].RemovedOperations.Kind != 0 {
			*errs = append(*errs, fmt.Errorf(msg, "tables."+name+".operations"))
		}
	}
}

// validateNullableVariants checks the wrapper-extraction fields on each
// NullableVariant: ValidField and ValidMethod are mutually exclusive,
// ValidInvert applies only to ValidMethod, and UnderlyingField requires
// a Nullable.Type to be set.
func validateNullableVariants(cfg *RootConfig, errs *[]error) {
	check := func(path string, n NullableVariant) {
		if n.ValidField != "" && n.ValidMethod != "" {
			*errs = append(*errs, fmt.Errorf("%s: specify at most one of valid_field, valid_method", path))
		}
		if n.ValidInvert && n.ValidMethod == "" {
			*errs = append(*errs, fmt.Errorf("%s: valid_invert only applies to valid_method", path))
		}
		if n.UnderlyingField != "" && n.Type == "" {
			*errs = append(*errs, fmt.Errorf("%s: underlying_field requires nullable.type", path))
		}
	}
	for sqlType, override := range cfg.Overrides.Types {
		check(fmt.Sprintf("overrides.types.%s.nullable", sqlType), override.Nullable)
	}
	for tableName, table := range cfg.Tables {
		if table.Overrides == nil {
			continue
		}
		for sqlType, override := range table.Overrides.Types {
			check(fmt.Sprintf("tables.%s.overrides.types.%s.nullable", tableName, sqlType), override.Nullable)
		}
	}
}

// validateCacheConfig validates the global cache block's fields and appends the
// key_prefix stderr warning when the default was applied.
func validateCacheConfig(cfg *RootConfig, errs *[]error, warnings *[]Warning) {
	if cfg.Cache == nil {
		return
	}
	c := cfg.Cache

	if c.Serializer != "" && !c.Serializer.IsValid() {
		*errs = append(*errs, fmt.Errorf("cache.serializer: %q is not a valid value (allowed: json, msgpack, custom)", c.Serializer))
	}
	if c.TTL != "" {
		if _, err := time.ParseDuration(c.TTL); err != nil {
			*errs = append(*errs, fmt.Errorf("cache.ttl: %q is not a valid Go duration: %w", c.TTL, err))
		}
	}
	if c.Hydration != nil && c.Hydration.Timeout != "" {
		if _, err := time.ParseDuration(c.Hydration.Timeout); err != nil {
			*errs = append(*errs, fmt.Errorf("cache.hydration.timeout: %q is not a valid Go duration: %w", c.Hydration.Timeout, err))
		}
	}
	validateCircuitBreakerConfig(c.CircuitBreaker, errs)

	if cfg.CacheKeyPrefixDefaulted {
		*warnings = append(*warnings, Warning{
			Message: `sqlgen: cache.key_prefix unset — using default "sqlgen". Set cache.key_prefix explicitly to silence this warning.`,
		})
	}
}

// validateCircuitBreakerConfig checks the circuit breaker bounds required by
// CACHE.md §3.4.
func validateCircuitBreakerConfig(cb *CircuitBreakerConfig, errs *[]error) {
	if cb == nil {
		return
	}
	if cb.FailureThreshold < 1 {
		*errs = append(*errs, fmt.Errorf("cache.circuit_breaker.failure_threshold: %d is not valid (must be >= 1)", cb.FailureThreshold))
	}
	if cb.ProbeInterval != "" {
		d, err := time.ParseDuration(cb.ProbeInterval)
		switch {
		case err != nil:
			*errs = append(*errs, fmt.Errorf("cache.circuit_breaker.probe_interval: %q is not a valid Go duration: %w", cb.ProbeInterval, err))
		case d <= 0:
			*errs = append(*errs, fmt.Errorf("cache.circuit_breaker.probe_interval: %q must be > 0", cb.ProbeInterval))
		}
	}
	if cb.HalfOpenMaxProbes < 1 {
		*errs = append(*errs, fmt.Errorf("cache.circuit_breaker.half_open_max_probes: %d is not valid (must be >= 1)", cb.HalfOpenMaxProbes))
	}
}

// validateViewCacheConfig enforces the view opt-in rule: an explicitly-enabled
// view cache must declare a non-empty invalidate_on list. TTL-only view caches
// are disallowed in v1 (PRD §27.11 / CACHE.md §3.4).
func validateViewCacheConfig(cfg *RootConfig, errs *[]error) {
	for name, view := range cfg.Views {
		if view.Cache == nil || view.Cache.Enabled == nil || !*view.Cache.Enabled {
			continue
		}
		if len(view.InvalidateOn) == 0 {
			*errs = append(*errs, fmt.Errorf("views.%s.cache.enabled is true but invalidate_on is empty; views must declare at least one source table (TTL-only view caches are not supported)", name))
		}
	}
}

// validateTenancyConfig validates the global, per-table, and per-view tenancy
// blocks against the YAML-parseable rules from PRD §29.2. The schema-dependent
// checks (per-entity `enabled: true` with a missing tenant column, and the
// view-only nullable-column rules) are handled later in the schema-resolution
// stage since the parsed schema is not available at pre-parse time.
//
// Views take the same TableTenancyConfig shape as tables (§4.9, §29.2.5), so
// the same type-override rule applies to them: a `type` with no `import` would
// reach the generator and emit an unimportable TenantResolver[T].
func validateTenancyConfig(cfg *RootConfig, errs *[]error) {
	if cfg.Tenancy != nil {
		if cfg.Tenancy.Enabled && cfg.Tenancy.Column == "" {
			*errs = append(*errs, fmt.Errorf("tenancy.column: required when tenancy.enabled is true"))
		}
		validateTenancyTypeOverride(cfg.Tenancy.Type, "tenancy.type", errs)
	}

	for name, table := range cfg.Tables {
		if table.Tenancy == nil {
			continue
		}
		validateTenancyTypeOverride(table.Tenancy.Type, fmt.Sprintf("tables.%s.tenancy.type", name), errs)
	}

	for name, view := range cfg.Views {
		if view.Tenancy == nil {
			continue
		}
		validateTenancyTypeOverride(view.Tenancy.Type, fmt.Sprintf("views.%s.tenancy.type", name), errs)
	}
}

// validateTenancyTypeOverride enforces the "type with no import" rule for a
// tenancy TypeOverride. The generator needs both fields to emit a valid Go
// import and reference for the tenant resolver (PRD §29.2.4 + §4.7).
func validateTenancyTypeOverride(t *TypeOverride, path string, errs *[]error) {
	if t == nil {
		return
	}
	if t.Type != "" && t.Import == "" {
		*errs = append(*errs, fmt.Errorf("%s: import is required when type is set (got type=%q)", path, t.Type))
	}
}

// validateSchemaDialect warns when input.schema is set for dialects that don't support schemas.
func validateSchemaDialect(cfg *RootConfig, warnings *[]Warning) {
	noSchemaDialect := cfg.Input.Dialect == DialectMySQL || cfg.Input.Dialect == DialectSQLite
	schemaSet := cfg.Input.Schema != "" && cfg.Input.Schema != "*"
	if noSchemaDialect && schemaSet {
		*warnings = append(*warnings, Warning{
			Message: fmt.Sprintf("input.schema is set to %q but %s does not support schemas; value will be ignored",
				cfg.Input.Schema, strings.ToUpper(string(cfg.Input.Dialect))),
		})
	}
}

// SchemaTable holds the minimal table information needed for post-parse validation.
//
// UniqueGroups carries column-name groupings covered by table-level UNIQUE
// constraints (CREATE UNIQUE INDEX, ADD CONSTRAINT ... UNIQUE (a, b)). Per-column
// inline UNIQUE shows up on SchemaColumn.Unique instead. Both feed the
// primary_key.columns "no UNIQUE coverage" validation.
type SchemaTable struct {
	Name         string
	Schema       string
	Columns      []SchemaColumn
	UniqueGroups [][]string
}

// SchemaView holds the minimal view information needed for post-parse validation.
// Views do not carry PK information: `@pk` annotations are honored by the
// generator but cursor_keys resolution never falls back to view columns — see
// PRD §4.13.
type SchemaView struct {
	Name    string
	Schema  string
	Columns []SchemaColumn
}

// SchemaColumn holds the minimal column information needed for post-parse validation.
type SchemaColumn struct {
	Name       string
	Type       string
	PrimaryKey bool
	Nullable   bool
	Unique     bool
	// HasDefault reports whether an INSERT may omit the column: it has a
	// DEFAULT expression or is GENERATED ALWAYS. Feeds the §32.4
	// required-on-create access rule.
	HasDefault bool
	// AutoIncrement reports serial / auto_increment columns.
	AutoIncrement bool
}

// ValidatePostParse validates the config against the parsed schema model.
// It returns all warnings and all errors aggregated together so the
// consumer can fix them in one pass.
func ValidatePostParse(cfg *RootConfig, tables []SchemaTable, views []SchemaView) ([]Warning, error) {
	var warnings []Warning
	var errs []error

	validateCursorKeys(cfg, tables, views, &errs, &warnings)
	validateSoftDeleteTypes(cfg, tables, &errs)
	validateExcludeColumnsExhaustion(cfg, tables, &warnings)
	validateAmbiguousBareNames(cfg, tables, &errs)
	validateViewInvalidateOn(cfg, tables, &errs)
	validatePrimaryKeyOverride(cfg, tables, &errs, &warnings)
	validateCompositePKStrategy(cfg, tables, &errs)
	validateMissingPrimaryKey(cfg, tables, &warnings)
	validateColumnAccessSchema(cfg, tables, &errs, &warnings)
	validateColumnOverridesSchema(cfg, tables, &errs)
	validateRelationshipDiscriminators(cfg, tables, &errs)
	validateRelationshipSortColumns(cfg, tables, &errs)
	validateRelationshipFKColumns(cfg, tables, &errs)

	return warnings, errors.Join(errs...)
}

// validateColumnAccessSchema enforces the schema-phase §32.4 access rules:
// the classified column must exist, PK members must stay API-readable,
// resolved cursor_keys members must stay API-readable, a required-on-create
// column may not be dropped from the API create input while the
// create/createMany API is enabled, and narrowing the soft-delete column's
// filter surface warns. The enum and excluded-column rules run pre-parse in
// validateColumnAccess.
func validateColumnAccessSchema(cfg *RootConfig, tables []SchemaTable, errs *[]error, warnings *[]Warning) {
	for _, table := range tables {
		tc := findTableConfig(cfg, table)
		if tc == nil || len(tc.ColumnMap) == 0 {
			continue
		}
		pkSet := resolvedPKSet(*tc, table)
		cursorKeys, cursorKeysFrom := resolvedCursorKeys(cfg, *tc, table)
		env := accessTableEnv{
			qn:             qualifiedTableName(table),
			cols:           make(map[string]SchemaColumn, len(table.Columns)),
			excluded:       make(map[string]bool),
			pkSet:          pkSet,
			cursorKeys:     cursorKeys,
			cursorKeysFrom: cursorKeysFrom,
			sdCol:          softDeleteColumnFor(cfg, table),
			createAPI:      tableCreateAPIEnabled(cfg, *tc),
			callerPK:       hasCallerStrategyPK(*tc, pkSet),
		}
		for _, col := range table.Columns {
			env.cols[col.Name] = col
		}
		for _, c := range ResolveTableExcludeColumns(*tc, cfg.Generation) {
			env.excluded[c] = true
		}
		for _, colName := range slices.Sorted(maps.Keys(tc.ColumnMap)) {
			validateOneColumnAccess(env, colName, tc.ColumnMap[colName].Access, errs, warnings)
		}
	}
}

// accessTableEnv carries the per-table facts the §32.4 column checks read.
type accessTableEnv struct {
	qn       string
	cols     map[string]SchemaColumn
	excluded map[string]bool
	pkSet    map[string]bool
	// cursorKeys is the table's resolved §4.13 cursor-key set, empty when no
	// cursor is ever encoded for it. cursorKeysFrom names where the set came
	// from, for the error message — an inherited default is the case where
	// the offending column is nowhere in the table's own config block.
	cursorKeys     map[string]bool
	cursorKeysFrom string
	sdCol          string
	createAPI      bool
	// callerPK reports whether the table's PK values are supplied by the
	// caller rather than generated server-side. When true, the PK columns
	// are part of the API create input (PRD §26.4) and therefore subject to
	// the required-on-create access rule like any other required column.
	callerPK bool
}

// hasCallerStrategyPK reports whether a table's primary key is caller-supplied,
// mirroring the load-bearing branches of gen.detectPKStrategy: an explicit
// `primary_key.strategy` wins, and absent that a composite (or absent) PK is
// unconditionally caller-strategy because it is a tuple of foreign keys with
// nothing to generate it. (`db` / `app` on a composite key is itself an error —
// validateCompositePKStrategy — so that pairing never reaches a valid config.)
//
// The single-column auto-detection branch is deliberately not replicated here
// — it needs the dialect plus the SQL-type classifiers that live in the gen
// package, and this validation only needs to be sound, not exhaustive. Two
// shapes are therefore not covered unless the config states the strategy
// explicitly: a single-column natural key (e.g. `code TEXT PRIMARY KEY`), and
// a single-column PK that is also a foreign key (e.g. the 1:1
// extension `profiles(user_id UUID PRIMARY KEY REFERENCES users(id))`). Both
// auto-detect to caller-strategy in gen; giving either an access role that
// drops it from the create input goes unreported here.
func hasCallerStrategyPK(tc TableConfig, pkSet map[string]bool) bool {
	if tc.PrimaryKey != nil && tc.PrimaryKey.Strategy != "" {
		return tc.PrimaryKey.Strategy == PKStrategyCaller
	}
	return len(pkSet) != 1
}

// validateOneColumnAccess applies the schema-phase §32.4 rules to a single
// column_map access entry.
//
// At most one error is reported per entry: the rules are ordered and each
// returns. The two structural-readability rules (PK, cursor_keys) come first
// because they hold regardless of API config, and PK precedes cursor_keys so
// a PK that is also a cursor key — the §4.13 fallback case, and the common
// explicit case — reports the PK rule rather than both.
func validateOneColumnAccess(env accessTableEnv, colName, access string, errs *[]error, warnings *[]Warning) {
	if !IsValidAccessRole(access) {
		return // unset, or invalid (already errored pre-parse)
	}
	col, ok := env.cols[colName]
	if !ok {
		if env.excluded[colName] {
			return // already errored pre-parse
		}
		*errs = append(*errs, fmt.Errorf(
			"tables.%s.column_map.%s.access: column %q does not exist on table %s",
			env.qn, colName, colName, env.qn,
		))
		return
	}
	apiReadable := access == AccessPublic || access == AccessReadOnly
	if env.pkSet[colName] && !apiReadable {
		*errs = append(*errs, fmt.Errorf(
			"tables.%s.column_map.%s.access: %q is not allowed on a primary-key column — PK columns must stay API-readable (allowed: public, read_only)",
			env.qn, colName, access,
		))
		return
	}
	if env.cursorKeys[colName] && !apiReadable {
		*errs = append(*errs, fmt.Errorf(
			"tables.%s.column_map.%s.access: %q is not allowed on a cursor_keys column (%s) — cursor key values are base64-encoded into every edge cursor and accepted back as after/before, so they must stay API-readable (allowed: public, read_only)",
			env.qn, colName, access, env.cursorKeysFrom,
		))
		return
	}
	if env.createAPI && accessDropsAPICreate(access) && columnRequiredOnCreate(env, colName, col) {
		*errs = append(*errs, fmt.Errorf(
			"tables.%s.column_map.%s.access: %q drops column %q from the API create input, but the column is required on create (NOT NULL, no default) and the create API is enabled — the API could never satisfy the INSERT (use write_only to keep it settable, or disable the create API)",
			env.qn, colName, access, colName,
		))
		return
	}
	if colName == env.sdCol && accessDropsFilter(access) {
		*warnings = append(*warnings, Warning{Message: fmt.Sprintf(
			"tables.%s.column_map.%s.access: %q removes the soft-delete column's filter from the API — the \"include soft-deleted rows\" path (PRD §26.5.3) is no longer reachable through the generated API",
			env.qn, colName, access,
		)})
	}
}

// columnRequiredOnCreate reports whether the API create input must carry the
// column for the INSERT to be satisfiable — NOT NULL, no default, and not
// filled in by the server.
//
// A PK column is normally exempt: its value is generated server-side (DB
// default / serial / app-minted UUID) and never comes from the API create
// input. A caller-strategy PK is the exception — the client supplies it, so
// dropping it from the create input leaves the INSERT writing zero-valued
// keys (PRD §26.4).
func columnRequiredOnCreate(env accessTableEnv, colName string, col SchemaColumn) bool {
	if col.Nullable || col.HasDefault || col.AutoIncrement {
		return false
	}
	return !env.pkSet[colName] || env.callerPK
}

// accessDropsAPICreate reports whether the role removes the column from the
// API create/update inputs (§32.2: only public and write_only are API-writable).
func accessDropsAPICreate(access string) bool {
	return access == AccessReadOnly || access == AccessHidden || access == AccessInternal
}

// accessDropsFilter reports whether the role removes the column from the API
// filter input (§32.2: only public and read_only are API-filterable).
func accessDropsFilter(access string) bool {
	return access == AccessWriteOnly || access == AccessHidden || access == AccessInternal
}

// resolvedPKColumns returns the table's effective primary-key columns in
// declaration order: the tables.<name>.primary_key.columns override when
// present (it silently replaces the auto-detected PK, PRD §8.6), otherwise
// the schema PK flags.
func resolvedPKColumns(tc TableConfig, table SchemaTable) []string {
	if tc.PrimaryKey != nil && len(tc.PrimaryKey.Columns) > 0 {
		return tc.PrimaryKey.Columns
	}
	return pkColumnNames(table.Columns)
}

// resolvedCursorKeys returns the table's effective §4.13 cursor-key set and a
// phrase naming where it came from, for the §32.4 error message.
//
// The set is empty when no cursor is ever encoded for the table, which is when
// §4.13 resolution omits the Connection method (nothing then generates
// encode<T>Cursor). Every other table generates Connection: the client has no
// operations toggle (PRD §4.6).
//
// The PK-fallback branch of §4.13 needs no source phrase of its own: its keys
// are PK columns, which validateOneColumnAccess reports under the PK rule
// before it reaches the cursor rule.
//
// Resolution runs against the post-exclude_columns column set, matching what
// the generator resolves against (gen.buildTableColumns drops excluded columns
// before gen.assembleTableContext calls ResolveTableConnection). Reading the
// raw set instead would keep an inherited key list alive that the generator
// discards in favour of the PK, and hard-error on a restricted sibling key no
// cursor ever carries.
func resolvedCursorKeys(cfg *RootConfig, tc TableConfig, table SchemaTable) (map[string]bool, string) {
	excluded := stringSet(ResolveTableExcludeColumns(tc, cfg.Generation))
	var present []string
	for _, name := range columnNames(table.Columns) {
		if !excluded[name] {
			present = append(present, name)
		}
	}
	decision := ResolveTableConnection(
		tc,
		cfg.Generation,
		qualifiedTableName(table),
		present,
		resolvedPKColumns(tc, table),
	)
	if decision.Err != nil || !decision.Emit {
		return nil, ""
	}
	switch {
	case len(tc.CursorKeys) > 0:
		return stringSet(decision.Keys), fmt.Sprintf("from tables.%s.cursor_keys", qualifiedTableName(table))
	case len(cfg.Generation.CursorKeys) > 0:
		return stringSet(decision.Keys), "inherited from generation.cursor_keys"
	default:
		// generation.cursor_keys is unset, so the keys come from the built-in
		// §4.13 default. Naming generation.cursor_keys here would point at a
		// setting the user never wrote.
		return stringSet(decision.Keys), "the built-in cursor_keys default"
	}
}

// resolvedPKSet returns resolvedPKColumns as a set.
func resolvedPKSet(tc TableConfig, table SchemaTable) map[string]bool {
	pk := make(map[string]bool)
	for _, name := range resolvedPKColumns(tc, table) {
		pk[name] = true
	}
	return pk
}

// softDeleteColumnFor returns the table's soft-delete column name, mirroring
// the first-match rule validateSoftDeleteTypes applies: the first configured
// soft_delete_columns entry present on the table wins. Empty when none match.
func softDeleteColumnFor(cfg *RootConfig, table SchemaTable) string {
	colTypes := columnTypeMap(table)
	for _, sd := range cfg.Generation.SoftDeleteColumns {
		if _, ok := colTypes[sd.Name]; ok {
			return sd.Name
		}
	}
	return ""
}

// tableCreateAPIEnabled reports whether the table's create or createMany
// mutation is exposed through the generated GraphQL API: the API must be on
// globally (api.enabled + api.graphql.enabled), the table must not opt out,
// and the resolved operations must enable create or createMany. Mirrors the
// BuildAPIContext emission gate.
func tableCreateAPIEnabled(cfg *RootConfig, tc TableConfig) bool {
	if cfg.API == nil || !cfg.API.Enabled || cfg.API.GraphQL == nil || !cfg.API.GraphQL.Enabled {
		return false
	}
	if tc.API != nil && tc.API.Enabled != nil && !*tc.API.Enabled {
		return false
	}
	return apiCreateOpsEnabled(cfg, tc)
}

// apiCreateOpsEnabled reports whether create / create_many survive the
// §26.5.1 `api.operations` mask. The client always generates both (PRD §4.6),
// so the mask alone decides. A mask that removes them means no create input
// reaches the API, so the §32.4 required-on-create access rule has nothing to
// protect and must not fire.
//
// An unresolvable preset reads as "off": it is already a pre-parse error and
// stacking a second misleading one on top helps nobody.
func apiCreateOpsEnabled(cfg *RootConfig, tc TableConfig) bool {
	mask, err := ResolveAPIOperationsMask(tc, cfg.API)
	if err != nil {
		return false
	}
	return mask == nil || opOn(mask.Create) || opOn(mask.CreateMany)
}

// opOn reads an operation flag, treating nil as off — the same convention the
// templates use when testing an operation.
func opOn(p *bool) bool { return p != nil && *p }

// validateCursorKeys delegates to the per-entity resolvers and surfaces their
// decisions as errors (explicit override pointing at a missing column) or
// warnings (inherited default forcing a Connection omission). Silent PK
// fallback produces neither — see PRD §4.13.
func validateCursorKeys(cfg *RootConfig, tables []SchemaTable, views []SchemaView, errs *[]error, warnings *[]Warning) {
	for _, table := range tables {
		var tc TableConfig
		if found := findTableConfig(cfg, table); found != nil {
			tc = *found
		}
		// resolvedPKColumns, not pkColumnNames: a table whose PK comes from
		// tables.<name>.primary_key.columns still has a PK to fall back on,
		// and the generator resolves it the same way (PRD §8.6). Reading the
		// raw flags warned "no primary key is available; Connection method
		// omitted" for every override table whose columns miss the inherited
		// cursor key, while gen went on to emit Connection anyway.
		decision := ResolveTableConnection(
			tc,
			cfg.Generation,
			qualifiedTableName(table),
			columnNames(table.Columns),
			resolvedPKColumns(tc, table),
		)
		if decision.Err != nil {
			*errs = append(*errs, decision.Err)
		}
		if decision.Warning != "" {
			*warnings = append(*warnings, Warning{Message: decision.Warning})
		}
	}

	for _, view := range views {
		vc := findViewConfig(cfg, view)
		decision := ResolveViewConnection(
			vc,
			cfg.Generation,
			qualifiedViewName(view),
			columnNames(view.Columns),
		)
		if decision.Err != nil {
			*errs = append(*errs, decision.Err)
		}
		if decision.Warning != "" {
			*warnings = append(*warnings, Warning{Message: decision.Warning})
		}
	}
}

// validateSoftDeleteTypes checks that soft delete columns have compatible SQL types.
// Compatible categories: "timestamp" (timestamp*, datetime), "bool" (bool*, tinyint).
func validateSoftDeleteTypes(cfg *RootConfig, tables []SchemaTable, errs *[]error) {
	sdCols := cfg.Generation.SoftDeleteColumns
	if len(sdCols) == 0 {
		return
	}

	for _, table := range tables {
		colTypes := columnTypeMap(table)
		for _, sd := range sdCols {
			sqlType, ok := colTypes[sd.Name]
			if !ok {
				continue // column doesn't exist in this table, try next
			}
			if !isSoftDeleteTypeCompatible(sd.Type, sqlType) {
				*errs = append(*errs, fmt.Errorf(
					"soft_delete_columns: column %q in table %s has incompatible type %q (expected %s-compatible type)",
					sd.Name, qualifiedTableName(table), sqlType, sd.Type,
				))
			}
			break // first match determines the soft delete column for this table
		}
	}
}

// validatePrimaryKeyOverride enforces the three rules for the
// tables.<name>.primary_key.columns override declared in PRD §4.13 / §8.6:
//
//  1. Every listed column must exist on the table (error).
//  2. Every listed column must be NOT NULL (error).
//  3. At least one UNIQUE constraint must cover the override column set; if
//     none does, emit a warning so the user knows sqlgen assumes uniqueness
//     enforced at the application level (PRD §8.6 — uniqueness may be
//     app-enforced, so this is a warning, not an error).
//
// Column-exclusion is applied before the column-existence check so an override
// pointing at an excluded column surfaces a clean missing-column error.
//
// When the override silently wins over an auto-detected PK (both present), no
// warning fires — the user's explicit declaration is authoritative (PRD §8.6).
func validatePrimaryKeyOverride(cfg *RootConfig, tables []SchemaTable, errs *[]error, warnings *[]Warning) {
	for _, table := range tables {
		tc := findTableConfig(cfg, table)
		if tc == nil || tc.PrimaryKey == nil || len(tc.PrimaryKey.Columns) == 0 {
			continue
		}

		qualified := qualifiedTableName(table)
		colByName := make(map[string]SchemaColumn, len(table.Columns))
		excludeSet := make(map[string]bool)
		for _, name := range ResolveTableExcludeColumns(*tc, cfg.Generation) {
			excludeSet[name] = true
		}
		for _, c := range table.Columns {
			if excludeSet[c.Name] {
				continue
			}
			colByName[c.Name] = c
		}

		var resolved []string
		var hadError bool
		for _, name := range tc.PrimaryKey.Columns {
			col, ok := colByName[name]
			if !ok {
				*errs = append(*errs, fmt.Errorf(
					"tables.%s.primary_key.columns: column %q does not exist on table %s",
					qualified, name, qualified,
				))
				hadError = true
				continue
			}
			if col.Nullable {
				*errs = append(*errs, fmt.Errorf(
					"tables.%s.primary_key.columns: column %q is nullable; primary key columns must be NOT NULL",
					qualified, name,
				))
				hadError = true
				continue
			}
			resolved = append(resolved, name)
		}

		if hadError || len(resolved) == 0 {
			continue
		}

		if !uniqueCovers(table, resolved) {
			*warnings = append(*warnings, Warning{
				Message: fmt.Sprintf(
					"tables.%s.primary_key.columns: no UNIQUE constraint covers the declared PK on table %s; sqlgen assumes uniqueness — enforce it at the application level if not at the DB level",
					qualified, qualified,
				),
			})
		}
	}
}

// validateCompositePKStrategy rejects `primary_key.strategy: db` or `app` on a
// table whose resolved primary key has more than one column (PRD §8.6: a
// composite key is always `caller`). The generator has no per-column strategy:
// Create, CreateMany, Upsert and UpsertMany build the returned key from the
// create input's PK fields, which `db` drops and `app` wraps in
// omittable.Value, so the generated package would not compile.
//
// The key is resolved the way the generator resolves it — a
// `primary_key.columns` override first, then the schema's PRIMARY KEY — so a
// composite key reached either way is covered. A table with no resolved key
// is skipped; it generates nothing.
func validateCompositePKStrategy(cfg *RootConfig, tables []SchemaTable, errs *[]error) {
	for _, table := range tables {
		tc := findTableConfig(cfg, table)
		if tc == nil || tc.PrimaryKey == nil {
			continue
		}
		strategy := tc.PrimaryKey.Strategy
		if strategy != PKStrategyDB && strategy != PKStrategyApp {
			continue
		}
		pk := resolvedPKColumns(*tc, table)
		if len(pk) < 2 {
			continue
		}
		qualified := qualifiedTableName(table)
		*errs = append(*errs, fmt.Errorf(
			"tables.%s.primary_key.strategy: %q is not supported on a composite primary key (%s) — a composite key is always caller-provided (PRD §8.6); remove the override, or declare a single-column key with primary_key.columns",
			qualified, strategy, strings.Join(pk, ", "),
		))
	}
}

// uniqueCovers reports whether any UNIQUE constraint (table-level UniqueGroups,
// inline column Unique, or a column listed as PrimaryKey at the column level)
// exactly matches the override column set. Set-equality is required: a covering
// UNIQUE must have the same column membership as the override (order-independent).
// A composite override matched by a single-column UNIQUE is NOT covered (a
// per-column UNIQUE on `(a)` does not make `(a, b)` unique).
func uniqueCovers(table SchemaTable, override []string) bool {
	want := make(map[string]bool, len(override))
	for _, c := range override {
		want[c] = true
	}
	if len(override) == 1 {
		for _, c := range table.Columns {
			if (c.Unique || c.PrimaryKey) && c.Name == override[0] {
				return true
			}
		}
	}
	for _, group := range table.UniqueGroups {
		if len(group) != len(want) {
			continue
		}
		match := true
		for _, c := range group {
			if !want[c] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// validateMissingPrimaryKey emits a warning for every table that has no PK
// after override resolution — auto-detection found none AND no
// tables.<name>.primary_key.columns override was supplied. Such tables are
// skipped from generation entirely (PRD §9.4b): no client, model struct,
// cache entry, or event hook is emitted. The warning prompts the user to
// either declare the PK explicitly via `primary_key.columns` or drop the
// table via the top-level `exclude_tables` filter.
func validateMissingPrimaryKey(cfg *RootConfig, tables []SchemaTable, warnings *[]Warning) {
	for _, table := range tables {
		tc := findTableConfig(cfg, table)
		if tc != nil && tc.PrimaryKey != nil && len(tc.PrimaryKey.Columns) > 0 {
			continue
		}
		hasAutoPK := false
		for _, c := range table.Columns {
			if c.PrimaryKey {
				hasAutoPK = true
				break
			}
		}
		if hasAutoPK {
			continue
		}
		if allColumnsExcluded(cfg, table) {
			// Table is being skipped entirely by exclude_columns — the
			// existing exhaustion warning already informs the user; no
			// need to also flag a missing PK on a skipped table.
			continue
		}
		if _, matched := MatchExcludeTablePattern(table.Name, table.Schema, cfg.ExcludeTables); matched {
			// Table is being excluded by the top-level exclude_tables filter
			// (PRD §6.4) — the warning tells users to use this
			// exact knob to silence itself, so honoring it here closes the
			// loop. BuildTableContexts also drops the table via the same
			// filter; this just keeps the validator warning consistent.
			continue
		}
		*warnings = append(*warnings, Warning{
			Message: fmt.Sprintf(
				"table %s has no primary key; skipped from generation. Use tables.%s.primary_key.columns to declare a PK or top-level exclude_tables to silence this warning.",
				qualifiedTableName(table), qualifiedTableName(table),
			),
		})
	}
}

// validateExcludeColumnsExhaustion warns when global + table-level exclude_columns
// removes every column from a table.
func validateExcludeColumnsExhaustion(cfg *RootConfig, tables []SchemaTable, warnings *[]Warning) {
	for _, table := range tables {
		if allColumnsExcluded(cfg, table) {
			*warnings = append(*warnings, Warning{
				Message: fmt.Sprintf("exclude_columns removes all columns from table %s; table will be skipped", qualifiedTableName(table)),
			})
		}
	}
}

// validateViewInvalidateOn verifies each cached view's invalidate_on entries
// name a parsed, generation-included table. An unresolved name or a name whose
// columns are fully excluded is a hard error (PRD §27.11 / CACHE.md §3.4) —
// no mutation hook would fire for it, so the view cache would silently never
// invalidate from that source.
func validateViewInvalidateOn(cfg *RootConfig, tables []SchemaTable, errs *[]error) {
	if len(cfg.Views) == 0 {
		return
	}

	parsed := make(map[string]SchemaTable, len(tables)*2)
	for _, t := range tables {
		parsed[t.Name] = t
		parsed[qualifiedTableName(t)] = t
	}

	for viewName, view := range cfg.Views {
		if view.Cache == nil || view.Cache.Enabled == nil || !*view.Cache.Enabled {
			continue
		}
		for _, src := range view.InvalidateOn {
			tbl, ok := parsed[src]
			if !ok {
				*errs = append(*errs, fmt.Errorf("views.%s.invalidate_on: table %q not found in parsed schema", viewName, src))
				continue
			}
			if allColumnsExcluded(cfg, tbl) {
				*errs = append(*errs, fmt.Errorf("views.%s.invalidate_on: table %q is excluded from generation (all columns filtered out); view cache would silently never invalidate from this source", viewName, src))
			}
		}
	}
}

// allColumnsExcluded reports whether every column on the table is filtered
// out by the union of global + table-level exclude_columns.
func allColumnsExcluded(cfg *RootConfig, table SchemaTable) bool {
	if len(table.Columns) == 0 {
		return false
	}
	excludeSet := make(map[string]bool)
	for _, col := range cfg.Generation.ExcludeColumns {
		excludeSet[col] = true
	}
	if tc := findTableConfig(cfg, table); tc != nil {
		for _, col := range tc.ExcludeColumns {
			excludeSet[col] = true
		}
	}
	if len(excludeSet) == 0 {
		return false
	}
	for _, col := range table.Columns {
		if !excludeSet[col.Name] {
			return false
		}
	}
	return true
}

// validateAmbiguousBareNames checks that bare (unqualified) config keys do not match
// tables in multiple schemas. Only applies to PostgreSQL — MySQL/SQLite have no schemas.
func validateAmbiguousBareNames(cfg *RootConfig, tables []SchemaTable, errs *[]error) {
	if cfg.Input.Dialect != DialectPostgres {
		return
	}

	// Build map: bare table name → set of schemas containing it.
	nameSchemas := make(map[string][]string)
	for _, table := range tables {
		nameSchemas[table.Name] = append(nameSchemas[table.Name], table.Schema)
	}

	for key := range cfg.Tables {
		if strings.Contains(key, ".") {
			continue // qualified key, no ambiguity
		}
		schemas := nameSchemas[key]
		if len(schemas) > 1 {
			*errs = append(*errs, fmt.Errorf(
				"ambiguous table reference %q: exists in schemas %s; use schema-qualified name (e.g., %s.%s)",
				key, strings.Join(schemas, ", "), schemas[0], key,
			))
		}
	}
}

// findTableConfig returns the TableConfig matching a schema table, or nil.
// It checks qualified name first, then bare name.
func findTableConfig(cfg *RootConfig, table SchemaTable) *TableConfig {
	qn := qualifiedTableName(table)
	if tc, ok := cfg.Tables[qn]; ok {
		return &tc
	}
	if tc, ok := cfg.Tables[table.Name]; ok {
		return &tc
	}
	return nil
}

// qualifiedTableName returns "schema.name" if schema is set, otherwise just "name".
func qualifiedTableName(table SchemaTable) string {
	if table.Schema == "" {
		return table.Name
	}
	return table.Schema + "." + table.Name
}

// qualifiedViewName returns "schema.name" if schema is set, otherwise just "name".
func qualifiedViewName(view SchemaView) string {
	if view.Schema == "" {
		return view.Name
	}
	return view.Schema + "." + view.Name
}

// findViewConfig returns the ViewConfig matching a schema view by qualified
// name first, then bare name. The zero ViewConfig is returned when no match
// exists.
func findViewConfig(cfg *RootConfig, view SchemaView) ViewConfig {
	qn := qualifiedViewName(view)
	if vc, ok := cfg.Views[qn]; ok {
		return vc
	}
	if vc, ok := cfg.Views[view.Name]; ok {
		return vc
	}
	return ViewConfig{}
}

// columnNames returns the flat list of column names from a slice of
// SchemaColumn values (tables and views share the column type).
func columnNames(cols []SchemaColumn) []string {
	names := make([]string, len(cols))
	for i, col := range cols {
		names[i] = col.Name
	}
	return names
}

// pkColumnNames returns the subset of column names marked PrimaryKey, in
// declaration order.
func pkColumnNames(cols []SchemaColumn) []string {
	var names []string
	for _, col := range cols {
		if col.PrimaryKey {
			names = append(names, col.Name)
		}
	}
	return names
}

// columnTypeMap returns a map of column name → SQL type for the table.
func columnTypeMap(table SchemaTable) map[string]string {
	m := make(map[string]string, len(table.Columns))
	for _, col := range table.Columns {
		m[col.Name] = col.Type
	}
	return m
}

// isSoftDeleteTypeCompatible checks if a SQL column type is compatible with the
// configured soft delete type category.
func isSoftDeleteTypeCompatible(configType SoftDeleteType, sqlType string) bool {
	lower := strings.ToLower(sqlType)
	switch configType {
	case SoftDeleteTimestamp:
		return strings.Contains(lower, "timestamp") || strings.Contains(lower, "datetime")
	case SoftDeleteBool:
		return strings.Contains(lower, "bool") || lower == "tinyint" || lower == "tinyint(1)"
	case SoftDeleteInteger:
		return strings.Contains(lower, "int") || strings.Contains(lower, "serial")
	default:
		return false
	}
}

// reservedManifestArtifactDirs lists subdirectories under output.dir that are
// emitted by other PRD §8.3 artifacts and therefore cannot be reused as the
// manifest markdown_dir. The set is small today (only `graph/` — the nested
// default GraphQL graph package `<output.dir>/graph` per PRD §26.5.8); future
// artifact dirs add entries here.
var reservedManifestArtifactDirs = map[string]bool{
	"graph": true,
}

// validateManifestConfig enforces the eight PRD §4.13 manifest validation
// rules. Tolerant of both raw and defaulted state — production calls go
// through LoadConfig (defaults already applied) but tests construct configs
// directly. The per-table opt-in check intentionally inspects raw pointer
// state so an explicit `enabled: true` with a global opt-out is caught
// regardless of whether the defaults pass has run.
func validateManifestConfig(cfg *RootConfig, errs *[]error) {
	m := cfg.Generation.Manifest
	enabled := m != nil && ptrVal(m.Enabled, false)

	if m != nil && enabled {
		validateManifestEnabledFields(m, errs)
	}
	if m != nil && !enabled {
		validateManifestDisabledFields(m, errs)
	}
	if m != nil {
		// Enforced regardless of enabled state: the removal path (manifest
		// disabled / emit flipped off) walks the same project_configs
		// targets, so the module-root-escape rule must hold symmetrically.
		validateManifestMCPTargets(m, errs)
	}
	validateManifestPerTableOptIn(cfg, enabled, errs)
}

// validateManifestEnabledFields covers the rules that only apply when
// manifest.enabled resolves to true.
func validateManifestEnabledFields(m *ManifestConfig, errs *[]error) {
	if len(m.Formats) == 0 {
		*errs = append(*errs, fmt.Errorf("manifest.formats: at least one format must be enabled when manifest.enabled is true (closed set: json, markdown) (PRD §30.2 / §4.13)"))
	}
	for _, f := range m.Formats {
		if f != ManifestFormatJSON && f != ManifestFormatMarkdown {
			*errs = append(*errs, fmt.Errorf("manifest.formats: unknown value %q (closed set: json, markdown) (PRD §30.2 / §4.13)", f))
		}
	}
	// embed_in_client embeds the on-disk JSON manifest onto the generated Client
	// (PRD §30.6), so json must be an emitted format. Rejecting the incoherent
	// combination here avoids rendering a manifest_embed_gen.go whose //go:embed
	// directive targets a JSON file that was never written. embed_in_client
	// defaults to true (§30.2), so an unset pointer counts as true. The
	// len(Formats) > 0 guard avoids double-reporting with the empty-formats rule.
	if ptrVal(m.EmbedInClient, true) && len(m.Formats) > 0 && !slices.Contains(m.Formats, ManifestFormatJSON) {
		*errs = append(*errs, fmt.Errorf("manifest.embed_in_client: requires %q in manifest.formats — embed_in_client embeds the on-disk JSON manifest; set embed_in_client: false or add json to formats (PRD §30.2 / §30.6 / §4.13)", ManifestFormatJSON))
	}
	if !m.JSONLayout.IsValid() {
		*errs = append(*errs, fmt.Errorf("manifest.json_layout: %q is not one of single, per_entity (PRD §30.2 / §4.13)", string(m.JSONLayout)))
	}
	validateManifestArtifactPaths(m, errs)
}

// validateManifestArtifactPaths enforces PRD §30.2's placement rules on the
// three fields that name where manifest artifacts land. They share a failure
// mode worth isolating: each is joined onto a parent directory by both the
// emitters and the stale-cleanup pass, so a value that escapes its parent makes
// sqlgen write — and prune — outside the tree it owns.
func validateManifestArtifactPaths(m *ManifestConfig, errs *[]error) {
	// §30.2 types json_filename as a "Filename ... relative to markdown_dir",
	// so it names a file inside that directory and nothing else. Separators
	// would put the emitted JSON — and the ownership anchor the stale sweep
	// reads back out of it — somewhere other than the manifest directory.
	if m.JSONFilename != "" && m.JSONFilename != filepath.Base(m.JSONFilename) {
		*errs = append(*errs, fmt.Errorf("manifest.json_filename: must be a bare filename, got %q — it is relative to markdown_dir and cannot contain a path separator (PRD §30.2 / §4.13)", m.JSONFilename))
	}
	if err := checkManifestDirPlacement("manifest.markdown_dir", m.MarkdownDir, true); err != nil {
		*errs = append(*errs, err)
	}
	if err := checkManifestDirPlacement("manifest.json_per_entity_dir", m.JSONPerEntityDir, false); err != nil {
		*errs = append(*errs, err)
	}
	if m.JSONPerEntityDir != "" && m.MarkdownDir != "" && m.JSONPerEntityDir == m.MarkdownDir {
		*errs = append(*errs, fmt.Errorf("manifest.json_per_entity_dir: cannot equal manifest.markdown_dir (%q) (PRD §30.2 / §4.13)", m.MarkdownDir))
	}
}

// validateManifestMCPTargets enforces the MCP.md §3.4 placement rules on
// manifest.mcp.project_configs: every target is a path relative to the module
// root and must not escape it — absolute paths and any `..` segment are
// rejected. Empty entries are rejected too (an empty string would resolve to
// the module root itself). Tolerant of a nil MCP block (defaults not applied).
func validateManifestMCPTargets(m *ManifestConfig, errs *[]error) {
	if m.MCP == nil {
		return
	}
	for _, target := range m.MCP.ProjectConfigs {
		if target == "" {
			*errs = append(*errs, fmt.Errorf("manifest.mcp.project_configs: entries cannot be empty (MCP.md §3.4)"))
			continue
		}
		if filepath.IsAbs(target) {
			*errs = append(*errs, fmt.Errorf("manifest.mcp.project_configs: %q must be relative to the module root, not absolute (MCP.md §3.4)", target))
			continue
		}
		if slices.Contains(strings.Split(filepath.ToSlash(target), "/"), "..") {
			*errs = append(*errs, fmt.Errorf("manifest.mcp.project_configs: %q must not escape the module root (`..` segments rejected) (MCP.md §3.4)", target))
		}
	}
}

// validateManifestDisabledFields rejects sub-flags whose explicit-true value
// is incoherent with manifest.enabled: false. The defaults pass only flips
// breadcrumbs / embed sub-flags on when enabled resolves true, so any true
// observed here is necessarily an explicit consumer setting.
func validateManifestDisabledFields(m *ManifestConfig, errs *[]error) {
	checks := []struct {
		field string
		val   *bool
	}{
		{"manifest.breadcrumbs.claude_md", m.Breadcrumbs.ClaudeMD},
		{"manifest.breadcrumbs.agents_md", m.Breadcrumbs.AgentsMD},
		{"manifest.breadcrumbs.package_doc", m.Breadcrumbs.PackageDoc},
		{"manifest.embed_in_client", m.EmbedInClient},
	}
	for _, c := range checks {
		if ptrVal(c.val, false) {
			*errs = append(*errs, fmt.Errorf("%s: cannot be true when manifest.enabled is false (PRD §30.2 / §4.13)", c.field))
		}
	}
	if m.MCP != nil && ptrVal(m.MCP.EmitProjectConfig, false) {
		*errs = append(*errs, fmt.Errorf("manifest.mcp.emit_project_config: cannot be true when manifest.enabled is false (MCP.md §3.4)"))
	}
}

// validateManifestPerTableOptIn rejects the opt-in form of the per-table
// override. The per-table flag is opt-out only — an explicit true with a
// global opt-out is a config error.
func validateManifestPerTableOptIn(cfg *RootConfig, globalEnabled bool, errs *[]error) {
	for _, name := range sortedTableKeys(cfg.Tables) {
		t := cfg.Tables[name]
		if t.Manifest == nil || t.Manifest.Enabled == nil {
			continue
		}
		if *t.Manifest.Enabled && !globalEnabled {
			*errs = append(*errs, fmt.Errorf("tables.%s.manifest.enabled: cannot be true when global manifest.enabled is false — per-table flag is opt-out only (PRD §30.2 / §4.13)", name))
		}
	}
}

// checkManifestDirPlacement enforces the placement rules for manifest dir
// fields: cannot be ".", cannot start with "_" (reserved for _index.md /
// _conventions.md), and (when checkArtifactSubdir is true) cannot collide
// with a known PRD §8.3 artifact subdir. Empty values are tolerated — the
// defaults pass fills them.
func checkManifestDirPlacement(field, value string, checkArtifactSubdir bool) error {
	if value == "" {
		return nil
	}
	if value == "." {
		return fmt.Errorf("%s: cannot be %q (PRD §30.2 / §4.13)", field, value)
	}
	// PRD §30.2 types both fields as a *subdirectory under* their parent
	// (markdown_dir under output.dir, json_per_entity_dir under markdown_dir).
	// An absolute path or one escaping via ".." is not a subdirectory, and the
	// emitters and the stale-cleanup pass both join it onto the parent — so a
	// value that escapes writes, prunes and removes outside the tree sqlgen
	// owns.
	if filepath.IsAbs(value) || value == ".." || strings.HasPrefix(filepath.ToSlash(value), "../") {
		return fmt.Errorf("%s: must be a subdirectory of its parent, got %q — absolute paths and %q escapes are rejected (PRD §30.2 / §4.13)", field, value, "..")
	}
	if strings.HasPrefix(value, "_") {
		return fmt.Errorf("%s: cannot start with %q (reserved for _index.md / _conventions.md) (PRD §30.2 / §4.13)", field, "_")
	}
	if checkArtifactSubdir {
		if reservedManifestArtifactDirs[strings.TrimSuffix(value, "/")] {
			return fmt.Errorf("%s: %q collides with a PRD §8.3 artifact subdir (PRD §30.2 / §4.13)", field, value)
		}
	}
	return nil
}
