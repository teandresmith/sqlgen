package gen

import (
	"fmt"
	"slices"
	"strings"
	"text/template"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/sql"
)

// FuncMap returns the custom template function registry for code generation.
// Equivalent to FuncMapWithResolver(dialect, nil) — funcmap helpers that
// consult user-declared Null-wrapper metadata fall back to built-in lookups
// only. Production code uses FuncMapWithResolver to feed sqlgen.yml-declared
// wrappers into the FK extraction path.
func FuncMap(dialect sql.Dialect) template.FuncMap {
	return FuncMapWithResolver(dialect, nil)
}

// FuncMapWithResolver returns the custom template function registry, threading
// the Resolver through helpers that need access to user-declared Null-wrapper
// extraction metadata. Functions remain pure and deterministic; the Resolver
// is read-only at template execution time.
func FuncMapWithResolver(dialect sql.Dialect, resolver *gotype.Resolver) template.FuncMap {
	return template.FuncMap{
		// Naming
		//
		// toSnakeCase is deliberately NOT registered. It reads a snake_case
		// name back out of a Go identifier, which is a guess (PRD §8.5) —
		// a template author reaching for a snake spelling next to
		// toPascalCase would pick the lossy helper over the exact one. No
		// template needs either today, and TEMPLATES.md §5 says not to
		// register speculatively; contexts carry SnakeName pre-computed.
		//
		// toPlural is NOT registered either, for the same reason in the other
		// direction: it returns an already-plural word unchanged, which is
		// what relationship field naming needs and what every template needs
		// the opposite of. A plural identifier that equalled its singular
		// emitted a duplicate GraphQL field and a duplicate Go method for a
		// table named `media` or `series`. structNamePlural is the guaranteed
		// spelling and the one `sqlgen lint` resolves against.
		"toPascalCase":     toPascalCase,
		"toCamelCase":      toCamelCase,
		"structNamePlural": StructNamePlural,
		"toSingular":       toSingular,
		"safeGoIdent":      safeGoIdent,

		// Types
		"goType":         funcGoType,
		"isNullableType": funcIsNullableType,
		"comparatorType": funcComparatorType(dialect),
		"zeroValue":      funcZeroValue,

		// Soft Delete
		"softDeleteSwitch":            funcSoftDeleteSwitch,
		"softDeleteValue":             funcSoftDeleteValue,
		"softDeleteCondition":         funcSoftDeleteCondition,
		"softDeleteRestoreCondition":  funcSoftDeleteRestoreCondition,
		"softDeleteTypeConst":         funcSoftDeleteTypeConst,
		"softDeleteRestoreGoValue":    funcSoftDeleteRestoreGoValue,
		"softDeleteFilterOverride":    funcSoftDeleteFilterOverride,
		"softDeleteSubqueryPredicate": funcSoftDeleteSubqueryPredicate,
		"softDeleteSubqueryArg":       funcSoftDeleteSubqueryArg,

		// PK Filtering
		"pkFilterExpr":      funcPKFilterExpr,
		"pkFilterInExpr":    funcPKFilterInExpr,
		"pkFilterInExprStr": funcPKFilterInExprStr,
		"pkIsStringType":    funcPKIsStringType,

		// Relationships
		"fkStringExpr":       funcFKStringExpr,
		"fkToString":         funcFKToString,
		"fkToStringByGoType": funcFKToStringByGoType,
		"fkNullGuard":        funcFKNullGuard,
		"fkLoadGuard":        funcFKLoadGuard(resolver),
		"fkLoadKey":          funcFKLoadKey(resolver),
		"isO2O":              funcIsO2O,
		"isM2M":              funcIsM2M,
		"reverseO2OTargets":  funcReverseO2OTargets,

		// Inputs
		"createInputFieldType": funcCreateInputFieldType,
		"updateInputFieldType": funcUpdateInputFieldType,

		// pkAutoGenType is deliberately NOT registered. The call a generated
		// create/upsert emits for an app-strategy primary key depends on the
		// UUID integration the *package* resolved (PRD §7.4 "Generating UUID
		// values"), which no single table context can see — so the expression
		// is pre-computed into TableContext.PKAutoGenExpr by
		// attachUUIDGeneration once every context exists, per the
		// pre-compute-everything rule in TEMPLATES.md §6.

		// Tags
		"formatTagPairs": funcFormatTagPairs,

		// Imports
		"uniqueImports": UniqueImports,

		// Dialect
		"placeholder":       funcPlaceholder(dialect),
		"quoteIdentifier":   funcQuoteIdentifier(dialect),
		"supportsReturning": funcSupportsReturning(dialect),

		// Cache / tenancy predicates
		"anyTenanted": funcAnyTenanted,

		// LockMode guard (PRD §9.6a)
		"lockGuardCtx": funcLockGuardCtx,

		// API / GraphQL helpers
		"screamingSnakeCase": funcScreamingSnakeCase,
		"gqlEnumIdent":       gqlEnumIdent,
		"hasAnyRead":         funcHasAnyRead,
		"hasAnyMutation":     funcHasAnyMutation,
		"pkConvert":          funcPKConvert,
		"pkSchemaArgs":       funcPKSchemaArgs,
		"pkGoArgs":           funcPKGoArgs,
		"pkPassArgs":         funcPKPassArgs,
		"inputCoerceBare":    funcInputCoerceBare,
		"inputCoerceDeref":   funcInputCoerceDeref,
	}
}

// funcInputCoerceBare returns the right-hand-side expression for an input
// translator assignment when the gqlgen-emitted Go field is BARE T (no
// pointer). Applies to schema-required fields (`T!` in GraphQL → bare T in
// gqlgen) and update-side bare branches.
//
// Wraps the value in a cast when the model's Go type differs from gqlgen's
// emitted bare type — most commonly the gqlgen-`int` → model-`intN` width
// adjustment, and equally the gqlgen-`float64` → model-`float32` one for a
// `real` column. AddressOf is also applied here so slice-typed scalars
// (json.RawMessage) flow through to the model's pointer wrapper, but only
// for nullable schema fields where the model wraps in a pointer; required
// json columns thread the slice through unchanged.
func funcInputCoerceBare(f APIInputField) string {
	expr := "in." + f.GoFieldName
	if f.CastTo != "" {
		expr = f.CastTo + "(" + expr + ")"
	}
	if f.AddressOf {
		expr = "&" + expr
	}
	return expr
}

// funcInputCoerceDeref returns the right-hand-side expression for an input
// translator assignment inside the nil-check guard for a nullable schema
// field. Four branches:
//
//   - AddressOf:           gqlgen emits BARE T (slice scalar like
//     `json.RawMessage` / `types.JSON`); model wraps `*T`. Take address.
//   - GqlgenIsBareNullable: gqlgen emits BARE T (slice/map scalar); model
//     stores bare T inside `omittable.Value[T]`. Pass through with no
//     deref — the value is already non-pointer.
//   - ModelInnerIsPointer: model wraps `*T`, gqlgen emits `*T`. Pass
//     through with no deref and no cast — but ONLY because the pointee types
//     agree. When they do not (a nullable narrowed numeric: gqlgen `*int` vs
//     model `*int32`, or gqlgen `*float64` vs model `*float32`), the
//     conversion cannot be written in expression position at all, so
//     `APIInputField.PointerCast` is set and the template emits a
//     deref-convert-readdress block instead of calling this helper.
//   - default:             model wraps T (or is bare T); gqlgen emits
//     `*T`. Deref and apply any size cast.
func funcInputCoerceDeref(f APIInputField) string {
	if f.AddressOf {
		return "&in." + f.GoFieldName
	}
	if f.GqlgenIsBareNullable {
		expr := "in." + f.GoFieldName
		// Named-slice bridge (e.g. gqlgen's `[]models.DocumentEntityTypeEnum`
		// → model's `models.DocumentEntityTypeEnumSlice`): the gqlgen-emitted
		// type is bare `[]T` but the model field uses a named slice, so the
		// cast still applies even though we didn't deref.
		if f.CastTo != "" {
			expr = f.CastTo + "(" + expr + ")"
		}
		return expr
	}
	if f.ModelInnerIsPointer {
		// Safe only when the pointee types match; PointerCast marks the case
		// where they do not, and the template handles it without this helper.
		return "in." + f.GoFieldName
	}
	expr := "*in." + f.GoFieldName
	if f.CastTo != "" {
		expr = f.CastTo + "(" + expr + ")"
	}
	return expr
}

// funcPKConvert emits the PK argument expression at a model-call boundary.
//
// For a single-PK table (`len(args) == 1`) it returns the bare arg name when
// the resolver's gqlgen-bound type matches the model client's expected type
// (e.g. uuid.UUID PK with gqlgen UUID→uuid.UUID binding) or a Go type
// conversion when they differ (e.g. `int → int64` for a `bigint` PK). The
// match path keeps generated output clean and avoids `unconvert` lint
// warnings on self-conversions.
//
// For a composite-PK table it constructs the model's PK struct
// literal — `models.<T>PK{Field1: arg1, Field2: <cast>(arg2), …}` — with
// per-arg casts inserted only where the resolver-side and model-side Go
// types differ. The unified client's `Get` / `Update` / `HardDelete` /
// `SoftDelete` / `Restore` methods take `pk <T>PK` for composite-PK tables,
// so emitting the literal at every call site is the simplest path; building
// a local var first is unnecessary.
func funcPKConvert(modelsPkg, structName string, args []APIPKArg) string {
	if len(args) == 0 {
		return ""
	}
	if len(args) == 1 {
		return convertOnePKArg(args[0])
	}
	var b strings.Builder
	b.WriteString(modelsPkg)
	b.WriteByte('.')
	b.WriteString(structName)
	b.WriteString("PK{")
	for i, a := range args {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(a.ModelFieldName)
		b.WriteString(": ")
		b.WriteString(convertOnePKArg(a))
	}
	b.WriteByte('}')
	return b.String()
}

// convertOnePKArg returns the Go expression for a single PK arg at the
// model-call boundary — `arg` when the gqlgen-bound and model Go types
// already match, or `<modelGoType>(arg)` otherwise.
func convertOnePKArg(a APIPKArg) string {
	if a.ModelGoType == a.GoType || a.ModelGoType == "" {
		return a.GraphQLName
	}
	return a.ModelGoType + "(" + a.GraphQLName + ")"
}

// funcPKSchemaArgs joins the PK args into a comma-separated GraphQL schema
// arg list (`userID: UUID!, categoryID: Int!`). Every PK column is non-null
// per SQL semantics so each entry carries a trailing `!`. Used in every
// per-PK schema field — `<table>(args)`, `update<T>(args, input)`,
// `delete<T>(args)`, etc.
func funcPKSchemaArgs(args []APIPKArg) string {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		parts = append(parts, a.GraphQLName+": "+a.GraphQLType+"!")
	}
	return strings.Join(parts, ", ")
}

// funcPKGoArgs joins the PK args into a comma-separated Go argument list
// (`userID uuid.UUID, categoryID int`). Used in resolver / seed method
// signatures so gqlgen's emitted *queryResolver / *mutationResolver
// interface declarations are satisfied.
func funcPKGoArgs(args []APIPKArg) string {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		parts = append(parts, a.GraphQLName+" "+a.GoType)
	}
	return strings.Join(parts, ", ")
}

// funcPKPassArgs joins the PK args into a comma-separated arg-passing list
// (`userID, categoryID`). Used in seed delegation bodies that forward the
// gqlgen-emitted resolver method args into the Q/M helper.
func funcPKPassArgs(args []APIPKArg) string {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		parts = append(parts, a.GraphQLName)
	}
	return strings.Join(parts, ", ")
}

// funcScreamingSnakeCase exposes screamingSnakeCase to templates. The
// generated sort enum reads APIFieldContext.SortEnumValue rather than
// calling this helper (PRD §26.5.3 — one derivation per surface); the
// registration survives for any template that needs the spelling, and
// because it delegates to the same helper the projection uses, a caller
// cannot reintroduce sort-enum drift on a digit-leading column.
func funcScreamingSnakeCase(s string) string {
	return screamingSnakeCase(s)
}

// funcHasAnyRead reports whether an APITableContext has at least one Query
// field to emit — the read-side twin of funcHasAnyMutation. Reads are maskable
// on tables and views alike (PRD §26.5.1 "Reads are maskable too"), so a mask
// that turns off get, connection, get_many and paginate leaves the
// `extend type Query { ... }` block with no field, and gqlgen rejects the empty
// extension ("expected at least one definition, found }"). The disjuncts are
// the template's three read lines, which tableQueryFieldNames mirrors.
func funcHasAnyRead(t APITableContext) bool {
	o := t.Operations
	return o.Get || o.Connection || o.Paginate
}

// funcHasAnyMutation reports whether an APITableContext has at least one
// flat mutation field that will actually emit (the nested `…WithRelated`
// fields have their own block and are not counted). The schema template only
// emits the flat `extend type Mutation { ... }` block when this is true
// (gqlgen rejects an empty extension).
//
// Create / Update / Upsert mutations also gate on HasCreateInput /
// HasUpdateInput because an all-PK table (e.g. a pure M2M junction with no
// metadata columns) has nothing to put in `Create<T>Input` / `Update<T>Input`,
// so those mutations are suppressed even when `api.operations` would otherwise
// keep them. Without this, a table with no usable Create/Update inputs and
// no delete/restore operations would emit an empty `extend type Mutation { }`
// block.
func funcHasAnyMutation(t APITableContext) bool {
	// A view emits no mutation, ever (PRD §16.4 / §26.4 "Views on the GraphQL
	// surface"). The read-only operation mask and the two false Has*Input flags
	// would already make flatMutationSurface false, but that is a coincidence of
	// how the view context happens to be built, not a rule anyone stated — and
	// a coincidence is the wrong thing to hang "this entity can never be
	// mutated over HTTP" on.
	if t.IsView {
		return false
	}
	return flatMutationSurface(t)
}

// mutationSurface is flatMutationSurface (what funcHasAnyMutation gates on,
// minus its IsView guard) plus the nested surface: does this context, read
// literally, describe an entity with at least one mutation field, flat or
// nested?
//
// It is separate so ValidateAPIViewReadOnly can ask that question of a view.
// Asking funcHasAnyMutation would be useless — the guard above answers `false`
// for every view before reaching flatMutationSurface, so the lint would be
// asserting the guard against itself. This is the expression that must
// actually stay false on a view; a future flat operation is added to
// flatMutationSurface, which this includes.
func mutationSurface(t APITableContext) bool {
	// A nested surface is up to three more mutations in its own `extend type
	// Mutation` block, so it counts here but not in funcHasAnyMutation, which
	// gates only the flat block.
	return flatMutationSurface(t) || t.Nested != nil
}

// flatMutationSurface reports whether the flat `extend type Mutation` block
// has at least one field. It does not count the nested surface: a nested
// upsert can stand alone on a table with no PK conflict target, which has no
// flat `upsert<T>`, and counting it would open an empty flat block
// that gqlgen rejects ("expected at least one definition").
func flatMutationSurface(t APITableContext) bool {
	o := t.Operations
	hasCreateMutation := (o.Create || o.CreateMany || (o.Upsert && t.HasConflictPK)) && t.HasCreateInput
	hasUpdateMutation := (o.Update || o.UpdateWhere) && t.HasUpdateInput
	return hasCreateMutation || hasUpdateMutation || o.HardDelete || o.SoftDelete || o.Restore
}

// LockGuardContext is the data shape passed into the shared/lock-mode-guard
// template fragment. Dialect determines which guard variant is emitted; OpName
// is interpolated into error messages so each call site reports the table-
// scoped operation that failed (e.g. "get product", "connection products").
type LockGuardContext struct {
	Dialect string
	OpName  string
}

// funcLockGuardCtx is the template helper that constructs a LockGuardContext
// from a TableContext and an op-scoped error prefix. The fragment lives in
// shared/_lock_mode_guard.tmpl.
func funcLockGuardCtx(tc TableContext, opName string) LockGuardContext {
	return LockGuardContext{
		Dialect: string(tc.Dialect),
		OpName:  opName,
	}
}

// funcAnyTenanted reports whether any cached table in the slice is tenanted.
// Used by the cache template to skip-emit tenant-only helpers when the
// project has no tenanted cached tables.
func funcAnyTenanted(tables []CachedTable) bool {
	for _, t := range tables {
		if t.Tenanted {
			return true
		}
	}
	return false
}

// --- Naming functions ---
// toCamelCase, StructNamePlural, toSingular are defined in naming.go.

// --- Type functions ---

// funcGoType returns the Go type expression for a column context.
func funcGoType(col ColumnContext) string {
	return col.GoType
}

// funcIsNullableType reports whether a Go type represents a nullable value.
func funcIsNullableType(goType string) bool {
	if strings.HasPrefix(goType, "*") {
		return true
	}
	if strings.HasPrefix(goType, "[]") || strings.HasPrefix(goType, "map[") {
		return true
	}
	if strings.HasPrefix(goType, "sql.Null") {
		return true
	}
	return false
}

// funcComparatorType returns a function that resolves the comparator type
// for a column context using the given dialect.
func funcComparatorType(dialect sql.Dialect) func(ColumnContext) string {
	return func(col ColumnContext) string {
		return resolveComparatorType(col, config.Dialect(dialect.Name()))
	}
}

// funcZeroValue returns the zero value expression for a column context.
func funcZeroValue(col ColumnContext) string {
	return col.ZeroValue
}

// --- Soft Delete functions ---

// funcSoftDeleteSwitch returns the SQL expression used to mark a row as
// soft-deleted, based on the soft delete strategy.
func funcSoftDeleteSwitch(strategy config.SoftDeleteType) string {
	switch strategy {
	case config.SoftDeleteTimestamp:
		return "CURRENT_TIMESTAMP"
	case config.SoftDeleteBool:
		return "TRUE"
	case config.SoftDeleteInteger:
		return "1"
	default:
		return "CURRENT_TIMESTAMP"
	}
}

// funcSoftDeleteValue returns the SQL expression used to restore a
// soft-deleted row (clear the soft delete column).
func funcSoftDeleteValue(strategy config.SoftDeleteType) string {
	switch strategy {
	case config.SoftDeleteTimestamp:
		return "NULL"
	case config.SoftDeleteBool:
		return "FALSE"
	case config.SoftDeleteInteger:
		return "0"
	default:
		return "NULL"
	}
}

// softDeleteWhere returns the sql.Where call that opens a soft delete
// condition. sql.Where writes its column verbatim, so the column is quoted
// through the client's dialect (CLAUDE.md rule 4); the expression
// assumes the generated client receiver `c`, which every caller has.
func softDeleteWhere(sd SoftDeleteContext) string {
	return fmt.Sprintf("sql.Where(c.dialect.QuoteIdentifier(%q))", sd.Column)
}

// funcSoftDeleteCondition returns the Go expression for the "is not deleted"
// SQL condition, based on the soft delete strategy. `W` below stands for
// sql.Where(c.dialect.QuoteIdentifier("col")).
//   - timestamp: W.IsNull()
//   - bool:      W.Eq(false)
//   - integer:   W.Eq(0)
func funcSoftDeleteCondition(sd SoftDeleteContext) string {
	col := softDeleteWhere(sd)
	switch sd.Strategy {
	case config.SoftDeleteBool:
		return col + ".Eq(false)"
	case config.SoftDeleteInteger:
		return col + ".Eq(0)"
	default:
		return col + ".IsNull()"
	}
}

// funcSoftDeleteSubqueryPredicate returns the SQL operator fragment appended to
// a quoted soft delete column inside a relationship filter's EXISTS subquery.
// The subquery is assembled as a string rather than as Conditions, so the
// comparison cannot come from sql.Where; the value, when there is one, still
// travels as an arg via funcSoftDeleteSubqueryArg.
//   - timestamp: " IS NULL"
//   - bool / integer: " = $"
func funcSoftDeleteSubqueryPredicate(sd SoftDeleteContext) string {
	switch sd.Strategy {
	case config.SoftDeleteBool, config.SoftDeleteInteger:
		return " = $"
	default:
		return " IS NULL"
	}
}

// funcSoftDeleteSubqueryArg returns the Go literal bound to the placeholder
// funcSoftDeleteSubqueryPredicate emits, or "" when the strategy needs none.
// Templates gate the append on the empty string.
func funcSoftDeleteSubqueryArg(sd SoftDeleteContext) string {
	switch sd.Strategy {
	case config.SoftDeleteBool:
		return "false"
	case config.SoftDeleteInteger:
		return "0"
	default:
		return ""
	}
}

// funcSoftDeleteRestoreCondition returns the Go expression for the "is deleted"
// SQL condition, used by Restore* methods to scope to deleted rows. `W` is
// the quoted sql.Where call from softDeleteWhere.
//   - timestamp: W.IsNotNull()
//   - bool:      W.Eq(true)
//   - integer:   W.Eq(1)
func funcSoftDeleteRestoreCondition(sd SoftDeleteContext) string {
	col := softDeleteWhere(sd)
	switch sd.Strategy {
	case config.SoftDeleteBool:
		return col + ".Eq(true)"
	case config.SoftDeleteInteger:
		return col + ".Eq(1)"
	default:
		return col + ".IsNotNull()"
	}
}

// funcSoftDeleteTypeConst returns the Go constant expression for the
// sql.SoftDeleteType value, based on the strategy string.
//   - timestamp: sql.SoftDeleteTimestamp
//   - bool:      sql.SoftDeleteBool
//   - integer:   sql.SoftDeleteInteger
func funcSoftDeleteTypeConst(strategy config.SoftDeleteType) string {
	switch strategy {
	case config.SoftDeleteBool:
		return "sql.SoftDeleteBool"
	case config.SoftDeleteInteger:
		return "sql.SoftDeleteInteger"
	default:
		return "sql.SoftDeleteTimestamp"
	}
}

// funcSoftDeleteRestoreGoValue returns the Go expression for the restore
// value used in setClauses map (the value that clears the soft delete column).
//   - timestamp: nil
//   - bool:      false
//   - integer:   0
func funcSoftDeleteRestoreGoValue(strategy config.SoftDeleteType) string {
	switch strategy {
	case config.SoftDeleteBool:
		return "false"
	case config.SoftDeleteInteger:
		return "0"
	default:
		return "nil"
	}
}

// funcSoftDeleteFilterOverride returns the Go expression for the comparator
// literal that overrides the default soft delete scoping in GetMany re-fetches.
// After a SoftDelete, the row IS deleted, so we need the filter to include it.
//
// The ComparatorType from FilterFieldContext includes a `*` prefix (for use in
// struct field declarations). This function strips it for the literal.
//
// Strategy-dependent output:
//   - Nullable types (NullableTime, NullableNumber, NullableBool): {Null: new(false)} — "IS NOT NULL"
//   - Bool: {Eq: new(true)} — "= true"
//   - Number: {Neq: new(0)} — "!= 0"
func funcSoftDeleteFilterOverride(f FilterFieldContext) string {
	ctype := strings.TrimPrefix(f.ComparatorType, "*")

	// Nullable comparators have a Null field — use it to override.
	if strings.HasPrefix(ctype, "comparator.Nullable") {
		return "&" + ctype + "{Null: new(false)}"
	}

	// Non-nullable Bool — use Eq: new(true) to match deleted rows.
	if ctype == "comparator.Bool" {
		return "&comparator.Bool{Eq: new(true)}"
	}

	// Non-nullable Number (integer soft delete) — use Neq: new(0) to match deleted rows.
	if strings.HasPrefix(ctype, "comparator.Number") {
		return "&" + ctype + "{Neq: new(0)}"
	}

	// Fallback (shouldn't happen for valid soft delete configs).
	return "&" + ctype + "{Null: new(false)}"
}

// --- Relationship functions ---

// funcFKStringExpr returns the Go expression to convert a FK value to a string
// for comparison. The expression uses $value as a placeholder for the value.
func funcFKStringExpr(col ColumnContext) string {
	switch col.FKConvert {
	case gotype.FKStringNone:
		return "$value"
	case gotype.FKStringStringer:
		return "$value.String()"
	case gotype.FKStringSprint:
		return "fmt.Sprint($value)"
	default:
		return "fmt.Sprint($value)"
	}
}

// funcPKFilterExpr returns a Go comparator filter expression for a PK column.
// For string PKs: &comparator.ID{Eq: new(id.String())}
// For numeric PKs: &comparator.Number[int64]{Eq: new(id)}
// For binary / network PKs: &comparator.Opaque[[]byte]{Eq: new(id)}
//
// The family is resolved by pkComparatorFamily, which mirrors
// resolveGenericComparator — the expression has to match the type of the
// filter field it is assigned into.
func funcPKFilterExpr(col ColumnContext, varExpr string) string {
	switch family, typeParam := pkComparatorFamily(col.GoType); family {
	case "Number":
		return fmt.Sprintf("&comparator.Number[%s]{Eq: new(%s)}", typeParam, varExpr)
	case "Opaque":
		// The operand is the column's own Go type, so it is passed through
		// rather than rendered — a binary or network key compared as text
		// matches nothing.
		return fmt.Sprintf("&comparator.Opaque[%s]{Eq: new(%s)}", typeParam, varExpr)
	}
	// String-based PK (UUID, text, etc.) — use comparator.ID with string conversion
	return fmt.Sprintf("&comparator.ID{Eq: new(%s)}", funcFKToString(col, varExpr))
}

// funcPKFilterInExpr returns a Go comparator In-filter expression for a PK or
// FK column. The `nullable` flag selects between the non-nullable comparator
// (Number/ID) and the nullable comparator (NullableNumber/NullableID); it is
// set true only when the *target* filter struct field is nullable, which
// happens for relationship loaders whose FK column on the target table allows
// NULL.
//
// For non-nullable string PKs: &comparator.ID{In: idStrings}
// For non-nullable numeric PKs: &comparator.Number[int64]{In: ids}
// For nullable string FKs: &comparator.NullableID{ID: comparator.ID{In: idStrings}}
// For nullable numeric FKs: &comparator.NullableNumber[int64]{Number: comparator.Number[int64]{In: ids}}
func funcPKFilterInExpr(col ColumnContext, varExpr string, nullable bool) string {
	baseGoType := strings.TrimPrefix(col.GoType, "*")
	return pkFilterInExpr(baseGoType, varExpr, nullable)
}

// funcPKFilterInExprStr is like funcPKFilterInExpr but works with a Go type string
// instead of a ColumnContext. Used for M2M relationships where only FKGoType is available.
func funcPKFilterInExprStr(goType, varExpr string, nullable bool) string {
	baseGoType := strings.TrimPrefix(goType, "*")
	return pkFilterInExpr(baseGoType, varExpr, nullable)
}

func pkFilterInExpr(baseGoType, varExpr string, nullable bool) string {
	switch family, typeParam := pkComparatorFamily(baseGoType); family {
	case "Number":
		if nullable {
			return fmt.Sprintf("&comparator.NullableNumber[%s]{Number: comparator.Number[%s]{In: %s}}", typeParam, typeParam, varExpr)
		}
		return fmt.Sprintf("&comparator.Number[%s]{In: %s}", typeParam, varExpr)
	case "Opaque":
		if nullable {
			return fmt.Sprintf("&comparator.NullableOpaque[%s]{Opaque: comparator.Opaque[%s]{In: %s}}", typeParam, typeParam, varExpr)
		}
		return fmt.Sprintf("&comparator.Opaque[%s]{In: %s}", typeParam, varExpr)
	}
	if nullable {
		return fmt.Sprintf("&comparator.NullableID{ID: comparator.ID{In: %s}}", varExpr)
	}
	return fmt.Sprintf("&comparator.ID{In: %s}", varExpr)
}

// funcPKIsStringType reports whether a PK column uses a string-based comparator
// (comparator.ID), and therefore whether the templates collect its keys as
// []string via fmt.Sprint / .String(). Numeric PKs use comparator.Number[T] and
// binary / network PKs use comparator.Opaque[T]; both take the key's own Go
// type, so neither needs — or tolerates — the string conversion.
// Accepts either a ColumnContext or a string Go type.
func funcPKIsStringType(v any) bool {
	var goType string
	switch t := v.(type) {
	case ColumnContext:
		goType = t.GoType
	case string:
		goType = t
	default:
		return true // fallback to string
	}
	family, _ := pkComparatorFamily(goType)
	return family == "ID"
}

// funcFKToString returns a Go expression that converts a FK value to a string,
// given a column context and the Go variable expression to convert.
// Example: funcFKToString(col, "id") → "id.String()" for UUID columns.
func funcFKToString(col ColumnContext, varExpr string) string {
	switch col.FKConvert {
	case gotype.FKStringNone:
		return varExpr
	case gotype.FKStringStringer:
		return varExpr + ".String()"
	case gotype.FKStringSprint:
		return "fmt.Sprint(" + varExpr + ")"
	default:
		return "fmt.Sprint(" + varExpr + ")"
	}
}

// funcFKToStringByGoType is funcFKToString driven by a Go type string instead
// of a ColumnContext. Used by relationship loaders for the M2M target side
// where only the target's PK Go type (RelationshipContext.FKGoType) is in
// scope — picking the conversion via the parent's PK FKConvert mismatches
// when target and parent PK types differ.
func funcFKToStringByGoType(goType, varExpr string) string {
	switch gotype.DeriveFKMethod(goType) {
	case gotype.FKStringNone:
		return varExpr
	case gotype.FKStringStringer:
		return varExpr + ".String()"
	case gotype.FKStringSprint:
		return "fmt.Sprint(" + varExpr + ")"
	default:
		return "fmt.Sprint(" + varExpr + ")"
	}
}

// funcFKLoadGuard returns a closure that produces the Go null-guard
// expression for a FK column, given its resolved Go type and the variable
// expression to guard. Returns "" when no guard is needed (non-null, bare
// type). Used by the O2M relationship loader to skip Null-wrapped
// FK rows whose .String() does not exist on the wrapper.
func funcFKLoadGuard(resolver *gotype.Resolver) func(string, string) string {
	return func(fkColumnGoType, varExpr string) string {
		ext := resolver.DeriveScalarExtraction(fkColumnGoType)
		if ext.GuardExpr == "" {
			return ""
		}
		return strings.ReplaceAll(ext.GuardExpr, "$v", varExpr)
	}
}

// funcFKLoadKey returns a closure that produces the bucket-key string
// expression for a FK column, given its resolved Go type and the variable
// expression to extract. Handles Null-wrapped struct FKs by emitting the
// wrapper's underlying-field access before applying the .String() / Sprint
// conversion.
func funcFKLoadKey(resolver *gotype.Resolver) func(string, string) string {
	return func(fkColumnGoType, varExpr string) string {
		ext := resolver.DeriveScalarExtraction(fkColumnGoType)
		unwrapped := strings.ReplaceAll(ext.UnwrapExpr, "$v", varExpr)
		switch ext.StringMethod {
		case gotype.FKStringNone:
			return unwrapped
		case gotype.FKStringStringer:
			return unwrapped + ".String()"
		default:
			return "fmt.Sprint(" + unwrapped + ")"
		}
	}
}

// funcFKNullGuard returns a Go null-guard expression for a nullable FK column.
// Returns an empty string if the column is not nullable.
func funcFKNullGuard(col ColumnContext) string {
	if !col.Nullable {
		return ""
	}
	if strings.HasPrefix(col.GoType, "*") {
		return "$value != nil"
	}
	if strings.HasPrefix(col.GoType, "sql.Null") {
		return "$value.Valid"
	}
	return ""
}

// funcIsO2O reports whether a relationship is one-to-one.
func funcIsO2O(rel RelationshipContext) bool {
	return rel.Type == parser.OneToOne
}

// funcIsM2M reports whether a relationship is many-to-many.
func funcIsM2M(rel RelationshipContext) bool {
	return rel.Type == parser.ManyToMany
}

// funcReverseO2OTargets returns the O2O targets in reverse order.
// Used by the relationships template for NULL detection assignments
// where deepest chain targets must be assigned before their parents.
func funcReverseO2OTargets(targets []O2OJoinDetail) []O2OJoinDetail {
	return reverseO2OTargets(targets)
}

// --- Input functions ---

// funcCreateInputFieldType returns the Go type for a field in CreateInput.
// Required fields use the bare type; optional fields use omittable.Value[T].
func funcCreateInputFieldType(field InputFieldContext) string {
	return field.GoType
}

// funcUpdateInputFieldType returns the Go type for a field in UpdateInput.
// All fields use omittable.Value[T].
func funcUpdateInputFieldType(field InputFieldContext) string {
	return field.GoType
}

// --- Dialect functions ---

// funcPlaceholder returns a function that produces a dialect-specific
// positional placeholder for the given position.
func funcPlaceholder(d sql.Dialect) func(int) string {
	return func(pos int) string {
		return d.Placeholder(pos)
	}
}

// funcQuoteIdentifier returns a function that quotes an identifier
// using the dialect's quoting convention.
func funcQuoteIdentifier(d sql.Dialect) func(string) string {
	return func(name string) string {
		return d.QuoteIdentifier(name)
	}
}

// funcSupportsReturning returns a function that reports whether the
// dialect supports RETURNING clauses.
func funcSupportsReturning(d sql.Dialect) func() bool {
	return func() bool {
		return d.SupportsReturning()
	}
}

// --- Tag functions ---

// funcFormatTagPairs formats a slice of TagPair into a Go struct tag string.
// Returns the backtick-enclosed tag string (e.g., `json:"name" db:"name"`).
func funcFormatTagPairs(tags []TagPair) string {
	if len(tags) == 0 {
		return ""
	}
	var parts []string
	for _, t := range tags {
		parts = append(parts, fmt.Sprintf("%s:%q", t.Key, t.Value))
	}
	return "`" + strings.Join(parts, " ") + "`"
}

// --- Import functions ---

// UniqueImports deduplicates and sorts a slice of import paths,
// filtering out empty strings.
func UniqueImports(imports []string) []string {
	seen := make(map[string]bool, len(imports))
	var result []string
	for _, imp := range imports {
		if imp == "" || seen[imp] {
			continue
		}
		seen[imp] = true
		result = append(result, imp)
	}
	slices.Sort(result)
	return result
}

// --- Formatting helpers used by funcmap functions ---

// FormatStructTags formats struct tags from column context.
func FormatStructTags(col ColumnContext) string {
	return fmt.Sprintf("`db:%q json:%q`", col.DBTag, col.JSONTag)
}
