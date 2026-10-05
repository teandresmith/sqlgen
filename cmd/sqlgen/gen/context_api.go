package gen

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/parser"
)

// --- API context types ---

// APIContext aggregates everything the api templates need to render. Each
// per-table context carries the data for its own .graphqls file; the shared
// fields (UsedScalars, ComparatorFamilies, FieldCasing) drive the shared
// .graphqls and the scalars_gen.go output.
type APIContext struct {
	Package         string
	SchemaDir       string
	ResolverDir     string
	FieldCasing     string
	Tables          []APITableContext
	UsedScalars     []APIScalarUse
	ExternalScalars []APIScalarUse // category-4 scalars only — drive scalars_gen.go
	// NumericWidthScalars lists the marshaler anchors sqlgen must emit into
	// scalars_gen.go so gqlgen can BIND a row-struct field whose numeric width
	// it ships no marshaler for (`int16`, `float32`, the unsigned family).
	// Sorted by Name; empty when every exposed numeric column lands on a width
	// gqlgen already binds. See numericWidthAnchor.
	NumericWidthScalars []APINumericWidth
	// ComparatorFamilies lists every `input <X>Comparator` block emitted
	// into shared_gen.graphqls, in schema order. It is the union of the
	// always-shipped families (shippedComparatorFamilies) and any further
	// family referenced by a column's APIFilterProjection, so the shared
	// schema always declares every input the per-table schemas reference.
	ComparatorFamilies []APIComparatorFamily
	// UsedEnums lists every schema-declared enum referenced by an API-exposed
	// column. Drives the `enum <Name> { ... }` declarations in
	// shared_gen.graphqls and the `<Name>: <modelsPkg>.<GoType>` entries in the
	// gqlgen `models:` merge. Sorted by GoTypeName; empty when no exposed
	// column is enum-typed.
	UsedEnums []APIEnumContext
	// ModelsPackage is the Go package name of the consumer's generated models
	// (e.g. "models"). Populated by the orchestrator from cfg.Output.Package
	// before resolver template render; the schema/scalars templates do not
	// reference this field. Tests may set it directly when invoking renders.
	ModelsPackage string
	// ModelsImportPath is the full Go import path for the consumer's generated
	// models package (e.g. "example.com/foo/gen"). Populated by the
	// orchestrator from go.mod + cfg.Output.Dir; empty when go.mod cannot be
	// resolved. The resolver template emits an empty import in that case so
	// the rendered file still parses, even though it will not compile until a
	// real module path is wired up.
	ModelsImportPath string
	// ClientName is the Go type name of the unified client struct
	// (e.g. "Client"), copied from cfg.Output.Client.Name.
	//
	// The resolvers template types the `Client` field of its Q and M helper
	// structs with it, and the `sqlgen graphql` seed templates use it for the
	// same qualified reference. gqlgen's one-shot resolver.go scaffold, not
	// sqlgen, owns the consumer's `type Resolver struct` declaration.
	ClientName string
	// SqlgenResolverPkgName is the Go package name of the sqlgen-owned helper
	// sub-package. Always "sqlgenresolver". Seed files in
	// the parent resolver package import this name to call InvalidInputError /
	// IncrementOp[T] etc.
	SqlgenResolverPkgName string
	// SqlgenResolverImportPath is the full Go import path for the
	// <resolver_dir>/sqlgenresolver/ helper sub-package. Populated by the
	// orchestrator from go.mod + cfg.API.GraphQL.ResolverDir + "sqlgenresolver".
	// Empty when the consumer module path cannot be resolved (same fail-soft
	// path as ModelsImportPath).
	SqlgenResolverImportPath string
	// UUIDParseFunc is the bare name of the string constructor the emitted
	// UUID / NullUUID scalar unmarshalers call — the selected UUID
	// integration's, because gofrs exports no Parse and the standard library
	// and google both do (PRD §7.4 "Parsing a UUID from a string", §26.4.1).
	//
	// Populated by BuildAPIContext and refined by the orchestrator once the
	// package's binding is known, the same shape selectUUIDIntegration's
	// fallback takes. Templates read it through ParseUUIDFunc, never
	// directly — see that method for why.
	UUIDParseFunc string
	// ResolverImportPath is the full Go import path for the consumer's
	// resolver-dir package (where scalars_gen.go lives). Populated from
	// go.mod + cfg.API.GraphQL.ResolverDir resolved against output.dir.
	// Used by the wrapper-merge to bind category-4 scalars (UUID/NullUUID/
	// Decimal/NullDecimal/JSON) to discovery-anchor symbols in this package
	// per PRD §26.4.1's Option B binding shape. Empty when the
	// consumer module path cannot be resolved (same fail-soft path as
	// ModelsImportPath / SqlgenResolverImportPath).
	ResolverImportPath string
	// GqlgenModelImportPath is the full Go import path for the consumer's
	// gqlgen-emitted model package, derived from gqlgen.yml's `model.filename`
	// (defaults to "<module>/graph/model"). Empty when gqlgen.yml is absent
	// or the consumer module path cannot be resolved (same fail-soft path
	// as ModelsImportPath). The seed / translator templates reference
	// gqlgen-emitted input types under GqlgenModelAlias.
	GqlgenModelImportPath string
	// GqlgenModelAlias is the Go import alias the seed / translator
	// templates use to qualify gqlgen-emitted input types like
	// `<alias>.Create<T>Input` / `<alias>.<T>Filter`. Always
	// "gqlmodel" when GqlgenModelImportPath is set, or empty otherwise.
	// Sqlgen-controlled (not derived from gqlgen.yml's `model.package`) so
	// no consumer-chosen value can collide with the alias spelling.
	GqlgenModelAlias string
	// ComparatorTranslators holds one entry per (family, T-parameter, nullable)
	// variant referenced by any column on any included table. Drives the
	// emission of graph/comparator_translate_gen.go (PRD §26.5.3 — comparator
	// translators are shared per family across the whole project, not per
	// table). Empty when no included table has a filterable column whose
	// comparator type maps to one of the supported GraphQL comparator
	// families (String / Numeric / Boolean / Time / ID).
	ComparatorTranslators []APIComparatorTranslator
}

// APITableContext holds per-table API generation data.
type APITableContext struct {
	StructName string // "Product" / "AuditUser"
	// SnakeName is the snake_case form of StructName, carried over from the
	// TableContext this was built from. It is the stem of the per-table
	// `<SnakeName>_gen.graphqls` schema and of the gqlgen resolver seed, so
	// reading one field keeps the two filenames from drifting apart
	// (PRD §8.5).
	SnakeName        string // "product" / "audit_user"
	StructNamePlural string // "Products" / "AuditUsers"
	QueryName        string // "product"
	QueryNamePlural  string // "products"
	ListQueryName    string // "productList"
	SQLTable         string // "products"
	Schema           string // "public" (empty if single-schema)
	// Description is the fully-resolved type-level doc block: the entity's own
	// comment when it has one, else the generated "<T> corresponds to the <sql>
	// table."/"… view." placeholder. Resolved here rather than in the schema
	// template so the noun follows IsView without the template branching on it
	// (guidelines/TEMPLATES.md §6 — contexts carry pre-computed data).
	Description string
	APIEnabled  bool
	// IsView marks this entry as a view rather than a table (PRD §26.4 "Views
	// on the GraphQL surface"). Views are first-class members of
	// APIContext.Tables — every downstream template, the walker, the envelope
	// aliases, the gqlgen `models:` merge, the resolver seeds and both
	// translators already iterate that one slice, and a parallel context type
	// would fork all of them.
	//
	// A view is read-only by construction: Operations is masked to the read
	// set, HasCreateInput / HasUpdateInput are false, and Relationships is nil
	// (§16.4 — a view has none). Mutation emission gates on funcHasAnyMutation,
	// which tests this flag first rather than inferring read-onlyness from that
	// combination holding by coincidence.
	IsView bool
	// ClientAccessor is the generated unified client's accessor method for this
	// entity — plural for a table (`Client.Products()`), singular for a view
	// (`Client.ProductSummary()`). Carried on the context rather than spelled
	// as StructNamePlural in the resolver template, because those two
	// spellings are the same only for tables (see entityAccessorName).
	ClientAccessor string
	Operations     ResolvedOperations
	HasSoftDelete  bool
	// HasConflictPK reports whether the table emits `<T>ConflictPK`
	// (PRD §9.5), which the flat `upsert<T>` mutation hard-codes (§26.5.1).
	// A key declared only through `tables.<name>.primary_key.columns`, with
	// no index behind it, emits no such constant, so the flat mutation is
	// omitted with it rather than emitted uncompilable.
	HasConflictPK bool
	PKType        string // "ID", "UUID", "Int" — used for query/mutation arg types when the table has a single PK column
	// PKArgs lists every PK column's resolver-side binding metadata. For a
	// single-PK table the slice has one entry and the schema/resolver/seed
	// templates emit a single arg (`<table>(id: <Type>!)`). For a composite-
	// PK table the slice has one entry per column and the templates
	// join them into `(arg1: T1!, arg2: T2!, …)` on every per-PK surface field
	// (`<table>(id)`, `update<T>`, `delete<T>` / `hardDelete<T>` /
	// `softDelete<T>`, `restore<T>`, `upsert<T>`). Resolver / seed bodies use
	// the `pkConvert` funcmap helper to construct `models.<T>PK{Field1: arg1,
	// Field2: arg2, …}` before calling the client when len(PKArgs) > 1, or to
	// pass `argName` (with optional cast) directly when len(PKArgs) == 1.
	PKArgs           []APIPKArg
	Fields           []APIFieldContext
	Relationships    []APIRelationshipContext
	HasNumericColumn bool
	UsedScalars      []string // scalars used in this table (for shared dedup)
	// IncrementEnumType is the generated Go enum-type name for the
	// IncrementInput's Column generic argument (e.g. "ProductIncrementColumn").
	// Empty when the table has no numeric non-PK columns.
	IncrementEnumType string
	// HasIncrementColumns is true iff the table has at least one non-PK
	// numeric column whose update path emits an `_inc` / `_dec` operator —
	// equivalently, iff `IncrementEnumType != ""`. Drives template gating
	// on the Update<Table> resolver and seed: when false, the
	// rendered method drops the `incOps` parameter and the increment-
	// dispatch loop, since no IncrementColumn enum was generated and
	// `IncrementOp[<empty>]` would not parse.
	HasIncrementColumns bool
	// UpdateOps is the per-numeric-column dispatch metadata for the §26.5.4
	// _set / _inc / _dec input operators. The resolver template walks this
	// slice to emit per-op runtime conflict checks and Increment dispatch.
	// Numeric columns only — non-numeric columns translate via plain set.
	UpdateOps []APIUpdateOp
	// CreateInputFields holds the gqlgen → model field translation metadata
	// for create-input bodies. PK columns excluded per §26.4 (they are not
	// part of the GraphQL input type).
	CreateInputFields []APIInputField
	// UpdateInputFields holds the gqlgen → model field translation metadata
	// for update-input bodies. All fields are omittable in the model layer.
	UpdateInputFields []APIInputField
	// HasCreateInput is true iff the table has ≥1 API-writable non-PK
	// column. Drives empty-input gating (extended by §32.2 access) — when false
	// the schema, resolver, seed, and translator templates skip emission of
	// `Create<Table>Input`, `create<Table>` / `create<Table>s` /
	// `upsert<Table>` mutations, and the matching translator helper. Without
	// this gate, an all-PK table (junction with no metadata columns) or a
	// table whose access roles drop every non-PK column from API-in emits an
	// empty `input Create<T>Input {}` definition, which gqlgen rejects
	// ("expected at least one definition, found }").
	HasCreateInput bool
	// HasUpdateInput mirrors HasCreateInput (≥1 API-writable non-PK column).
	// The same empty-input gating applies to `Update<Table>Input`, `update<Table>` /
	// `update<Table>s` mutations, and the matching translator helper.
	HasUpdateInput bool
	// FilterFields holds the gqlgen → model translation metadata for the
	// per-table filter translator (PRD §26.5.3). One entry per non-PK column
	// whose comparator type maps to a supported GraphQL comparator family.
	// Columns whose comparator type is unsupported (e.g. Slice/Enum/JSON)
	// are dropped from the per-table translator body and silently ignored
	// at runtime — the GraphQL schema still accepts them via the StringComparator
	// fallback, but the model-side filter receives nil for those fields.
	FilterFields []APIFilterField
	// FilterRelationships holds the relationship members on the `<T>Filter`
	// input — one per model-side relationship filter whose target is itself
	// exposed on the API surface (PRD §26.4, §26.5.3). Always empty for a
	// view: §16.4 gives views no relationships, so there is nothing to project.
	FilterRelationships []APIFilterRelationship
	// SortEnumGoType is the gqlgen-generated Go type name for the sort enum
	// (e.g. "ProductSortField"). Empty when the table emits no sort
	// translator (no columns to sort by).
	SortEnumGoType string
	// SortInputType is the gqlgen-generated Go input type name for the per-
	// table sort input (e.g. "ProductSort"). gqlgen v0.17.x mirrors the GraphQL
	// input type name verbatim — `input ProductSort { ... }` becomes Go
	// `ProductSort`, with no "Input" suffix appended. Empty when no sort
	// translator is emitted.
	SortInputType string
	// SortFields holds one entry per column the sort enum exposes. Drives
	// the case clauses inside <table>SortFieldToColumn. Sourced in column-
	// declaration order so the GraphQL enum and Go switch align.
	SortFields []APISortField
	// RowIdentityFields names the FieldOptions fields the walker sets when a
	// selection set asks for the row but names no column. See
	// rowIdentityFields for how the set is chosen; empty only for an entity
	// with no columns at all, which the walker then leaves untouched.
	RowIdentityFields []string
	// Nested is the GraphQL projection of the table's nested-mutation surface
	// (PRD §9.9, §26.5.1) — the three `…WithRelated` mutations, their wrapper
	// and verb-block inputs, and the upsert's conflict-target enum. nil when
	// there is nothing to project, and always nil on a view (§16.4). Attached
	// by wireAPINestedMutations once every table's context exists, because an
	// edge is gated on its target's API context.
	Nested *APINestedContext
}

// APIComparatorTranslator describes one comparator translator function
// emitted by graph/comparator_translate_gen.go. A schema referencing a
// nullable int32 column produces an entry with Family="Number",
// Nullable=true, NumericT="int32"; the template renders it as
// `translateNullableNumericComparatorInt32(*NullableNumericComparator) *comparator.NullableNumber[int32]`.
//
// Translators are shared across the whole project per PRD §26.5.3 — every
// per-table filter translator dispatches into them by FuncName.
type APIComparatorTranslator struct {
	// FuncName is the generated Go function name (e.g.
	// "translateNumericComparatorInt32"). Unique across the whole file —
	// used as the dedup key during context build.
	FuncName string
	// InputTypeName is the gqlgen-generated Go input type name passed in
	// as the parameter — "StringComparator" for a base variant,
	// "NullableStringComparator" for a nullable one. The two are distinct
	// GraphQL input types (PRD §26.4 Rule 2), so a nullable translator
	// cannot delegate to its base by forwarding the same value.
	InputTypeName string
	// Family is the comparator family — one of "String", "Number", "Bool",
	// "Time", "ID", "Decimal", "Opaque", "Enum", "JSON", "JSONB", "Slice".
	// Drives which template branch
	// emits the function body. Note it names the branch, not the GraphQL
	// input: a duration column carries Family "Number" because its model
	// comparator really is comparator.Number[time.Duration] and the numeric
	// operand block emits it verbatim — only InputTypeName and FuncName
	// differ.
	Family string
	// Nullable distinguishes the *comparator.X variant (false) from the
	// *comparator.NullableX wrapper (true). A nullable variant takes its own
	// Nullable<X>Comparator input and copies the operands directly onto the
	// wrapper — it cannot delegate to the base translator, because Rule 2
	// makes the two distinct GraphQL input types and gqlgen therefore emits
	// distinct Go types. The copy costs nothing: comparator.Nullable<X>
	// embeds <X>, so one operand-assignment block compiles against either
	// receiver through field promotion. The variant additionally carries the
	// input's `isNull` operand into the wrapper's `Null` field.
	Nullable bool
	// NumericT is the Go T parameter for Number / NullableNumber variants
	// (e.g. "int32", "float64"). Empty for non-Numeric families.
	NumericT string
	// OpaqueT is the Go T parameter for Opaque / NullableOpaque variants
	// (e.g. "[]byte", "net.IP"). Empty for every other family.
	OpaqueT string
	// EnumT is the Go T parameter for Enum / NullableEnum variants, already
	// qualified with the consumer's models package alias (e.g.
	// "models.DocumentEntityTypeEnum"). Empty for every other family.
	//
	// It is qualified where NumericT and OpaqueT are not because an enum's
	// Go type is the consumer's, not a builtin or a stdlib type the graph
	// package can name directly — see typeBindings.modelsPkg.
	EnumT string
	// SliceT is the Go element type of a Slice / NullableSlice variant's type
	// parameter, spelled as the generated graph package must spell it —
	// models-qualified when the element is a schema enum (SliceElemIsEnum),
	// bare otherwise. Empty for every other family.
	SliceT string
	// SliceElemGraphQL is the element's GraphQL type (`String`, `Int`, a bound
	// enum). It names the input this translator takes — the input is keyed on
	// the operand type while the translator is keyed on the Go type, so the
	// two are carried separately (PRD §26.4 "Sharing").
	SliceElemGraphQL string
	// SliceElemCast says gqlgen's list element type differs from SliceT and
	// each element needs a Go conversion. True exactly for the numeric widths
	// `Int` / `Float` collapse onto — the same licensed disagreement
	// gqlgenNumericWidthCasts records — and false for every identity copy.
	SliceElemCast bool
	// SliceElemIsEnum marks a slice whose element is a schema enum, which is
	// the one Slice shape naming a type from the consumer's models package.
	SliceElemIsEnum bool
	// OperandIsPointer says the gqlgen input's scalar operand fields arrive
	// as `*T` rather than bare `T`, and ListElemIsPointer says its list
	// operands arrive as `[]*T` rather than `[]T`. Both are properties of
	// gqlgen's binder, not of the comparator, and both are MEASURED against
	// gqlgen v0.17.90 rather than inferred — see opaqueComparatorBindings for
	// the table and the counterexample that stops it being derived from one
	// flag. Meaningful only for the Opaque family; the other families spell
	// their operand shapes directly in their own template arms.
	OperandIsPointer  bool
	ListElemIsPointer bool
	// GoReturnType is the full Go return type expression
	// (e.g. "*comparator.Number[int32]").
	GoReturnType string
	// OutStructName is the struct the generated function allocates, without
	// the leading `*` (e.g. "comparator.String",
	// "comparator.NullableNumber[int32]"). Operand assignments are spelled
	// the same either way because Nullable<X> embeds <X>, so Go promotes
	// `out.Eq` onto the embedded struct — which is what lets one template
	// fragment serve both variants of a family.
	OutStructName string
}

// APIComparatorOperator is one operator field on a generated comparator
// input (PRD §26.4 "Comparator input projection"). Name is the GraphQL
// field name (`eq`, `startsWith`, `between`); Type is the GraphQL type of
// its operand, already spelled in schema syntax (`String`, `[Float!]`,
// `TimeRange`).
type APIComparatorOperator struct {
	Name string
	Type string
}

// APIComparatorFamily is one `input <X>Comparator { ... }` block emitted
// into shared_gen.graphqls. The shared-schema template renders the whole
// comparator surface from APIContext.ComparatorFamilies rather than from
// hard-coded blocks, so adding a family is a data change, not a template
// change (PRD §26.4 Rule 1 — monomorphization).
type APIComparatorFamily struct {
	// Name is the GraphQL input type name (e.g. "StringComparator").
	// Matches APIFilterProjection.InputTypeName for every column that
	// references this family.
	Name string
	// Operators lists the family's operator fields in emission order.
	Operators []APIComparatorOperator
	// RangeInputName names the companion `input <X>Range` block emitted
	// immediately after this family (e.g. "NumericRange"), or "" when the
	// family declares no range operator. The range input is a separate
	// GraphQL input type, so it cannot be folded into Operators.
	RangeInputName string
	// RangeOperandType is the GraphQL type of the range input's `from` /
	// `to` fields (e.g. "Float"). Both are non-null. Empty when
	// RangeInputName is empty.
	RangeOperandType string
	// NameWidth is the column width the shared-schema template pads each
	// operator's `<name>:` label to, so operand types line up inside the
	// block. Pre-computed by collectComparatorFamilies from the longest
	// operator name — the template does layout, not arithmetic.
	NameWidth int
}

// comparatorNameWidth returns the padding width for a family's operator
// labels: the longest `<name>:` in the set.
func comparatorNameWidth(ops []APIComparatorOperator) int {
	w := 0
	for _, op := range ops {
		if n := len(op.Name) + 1; n > w {
			w = n
		}
	}
	return w
}

// APIFilterProjection is a column's resolved GraphQL filter projection —
// the single derivation the schema emitter and the filter translator both
// read (PRD §26.5.3: one field map, two emitters).
//
// Before this existed the two surfaces derived the same fact from different
// inputs: the schema switched on the column's *GraphQL* type and the
// translator parsed the model's *Go* `*comparator.X[Y]` expression. When
// they disagreed the schema won at parse time and the translator won at
// execution time, so the field was accepted and silently ignored — the
// server returned the unfiltered set while the client believed it had
// filtered. Resolving both from one place makes that failure structurally
// unavailable: there is nothing left to disagree with.
//
// The zero value means "this column contributes no filter field at all",
// which is the `json[]` / `jsonb[]` and §32.2 (access role drops
// the filter surface) case. Both emitters skip on the same emptiness, so
// neither can outrun the other.
type APIFilterProjection struct {
	// InputTypeName is the GraphQL input type the schema advertises for
	// this column (e.g. "StringComparator"). Empty exactly when the column
	// contributes no filter field — and, since a projection is only ever
	// resolved from a translator that exists, empty exactly when
	// TranslatorFunc is empty too.
	//
	// That biconditional is the one-field-map invariant in its narrowest form:
	// the pair is populated together or empty together, never one without the
	// other. ValidateAPIFilterCompleteness (PRD §26.5.3) is the guard that
	// keeps it so from the outside, checking the emitted surfaces rather than
	// this struct.
	InputTypeName string
	// TranslatorFunc names the comparator translator the per-table filter
	// dispatches into (e.g. "translateNullableTimeComparator"). Empty when
	// no translator can handle the column's model comparator — in which case
	// the whole projection is zero and the column is absent from the schema
	// input as well as from the translator body.
	TranslatorFunc string
	// Translator is the full descriptor TranslatorFunc names, carried so
	// the project-wide translator registry is populated from this same
	// resolution instead of re-parsing the comparator type expression at a
	// second decision point. Zero value when TranslatorFunc is empty.
	Translator APIComparatorTranslator
	// Operators is the operator list of the family InputTypeName names,
	// copied from the family table. Drives the shared-schema declaration
	// for families outside the always-shipped set.
	Operators []APIComparatorOperator
}

// shippedComparatorFamilies is the comparator surface a generated project
// can declare, in schema-emission order. This table is the single source of
// both the `input <X>Comparator` blocks in shared_gen.graphqls and the
// operator list carried on each column's APIFilterProjection — the operator
// sets are not restated in the template.
//
// Each entry is the `NOT NULL` form. Its `Nullable<X>Comparator` twin is
// derived by nullableComparatorTwin rather than listed here: GraphQL input
// types are global, so nullability cannot be a conditional field on a shared
// input and has to be a separate type carrying the same operands plus
// `isNull` (PRD §26.4 Rule 2). Both forms are emitted only when a column
// actually references them (see collectComparatorFamilies).
//
// Operator sets project their Go counterpart in `comparator/` faithfully,
// minus the `Custom` field every family carries — that is a raw-SQL escape
// hatch and exposing it over HTTP would be an injection surface (PRD §26.4).
// DecimalComparator is the one deliberate narrowing: a decimal column filters
// through `comparator.String` on the canonical string form, but the text
// operators are meaningless against a numeric column, so only the ordered and
// set operators are projected.
var shippedComparatorFamilies = []APIComparatorFamily{
	{
		Name: "StringComparator",
		Operators: []APIComparatorOperator{
			{Name: "eq", Type: "String"},
			{Name: "neq", Type: "String"},
			{Name: "gt", Type: "String"},
			{Name: "gte", Type: "String"},
			{Name: "lt", Type: "String"},
			{Name: "lte", Type: "String"},
			{Name: "contains", Type: "String"},
			{Name: "startsWith", Type: "String"},
			{Name: "endsWith", Type: "String"},
			{Name: "like", Type: "String"},
			{Name: "nlike", Type: "String"},
			{Name: "in", Type: "[String!]"},
			{Name: "nin", Type: "[String!]"},
		},
	},
	{
		Name: "NumericComparator",
		Operators: []APIComparatorOperator{
			{Name: "eq", Type: "Float"},
			{Name: "neq", Type: "Float"},
			{Name: "gt", Type: "Float"},
			{Name: "gte", Type: "Float"},
			{Name: "lt", Type: "Float"},
			{Name: "lte", Type: "Float"},
			{Name: "in", Type: "[Float!]"},
			{Name: "nin", Type: "[Float!]"},
			{Name: "between", Type: "NumericRange"},
			{Name: "nbetween", Type: "NumericRange"},
		},
		RangeInputName:   "NumericRange",
		RangeOperandType: "Float",
	},
	{
		Name: "BooleanComparator",
		Operators: []APIComparatorOperator{
			{Name: "eq", Type: "Boolean"},
			{Name: "neq", Type: "Boolean"},
		},
	},
	{
		Name: "TimeComparator",
		Operators: []APIComparatorOperator{
			{Name: "eq", Type: "Time"},
			{Name: "neq", Type: "Time"},
			{Name: "gt", Type: "Time"},
			{Name: "gte", Type: "Time"},
			{Name: "lt", Type: "Time"},
			{Name: "lte", Type: "Time"},
			{Name: "in", Type: "[Time!]"},
			{Name: "nin", Type: "[Time!]"},
			{Name: "between", Type: "TimeRange"},
			{Name: "nbetween", Type: "TimeRange"},
		},
		RangeInputName:   "TimeRange",
		RangeOperandType: "Time",
	},
	{
		Name: "IDComparator",
		Operators: []APIComparatorOperator{
			{Name: "eq", Type: "ID"},
			{Name: "neq", Type: "ID"},
			{Name: "gt", Type: "ID"},
			{Name: "gte", Type: "ID"},
			{Name: "lt", Type: "ID"},
			{Name: "lte", Type: "ID"},
			{Name: "in", Type: "[ID!]"},
			{Name: "nin", Type: "[ID!]"},
		},
	},
	{
		Name: "DecimalComparator",
		Operators: []APIComparatorOperator{
			{Name: "eq", Type: "Decimal"},
			{Name: "neq", Type: "Decimal"},
			{Name: "gt", Type: "Decimal"},
			{Name: "gte", Type: "Decimal"},
			{Name: "lt", Type: "Decimal"},
			{Name: "lte", Type: "Decimal"},
			{Name: "in", Type: "[Decimal!]"},
			{Name: "nin", Type: "[Decimal!]"},
		},
	},
	// PRD §26.4 "Duration columns". A duration column's model comparator IS
	// comparator.Number[time.Duration] (`time.Duration` is `~int64`, so it
	// satisfies comparator.Numeric), but `time.Duration` binds to the
	// `Duration` scalar rather than to `Float` — so by Rule 1's own keying
	// on the GraphQL operand type it splits back out of NumericComparator
	// instead of collapsing into it, and carries its own range input.
	{
		Name: "DurationComparator",
		Operators: []APIComparatorOperator{
			{Name: "eq", Type: "Duration"},
			{Name: "neq", Type: "Duration"},
			{Name: "gt", Type: "Duration"},
			{Name: "gte", Type: "Duration"},
			{Name: "lt", Type: "Duration"},
			{Name: "lte", Type: "Duration"},
			{Name: "in", Type: "[Duration!]"},
			{Name: "nin", Type: "[Duration!]"},
			{Name: "between", Type: "DurationRange"},
			{Name: "nbetween", Type: "DurationRange"},
		},
		RangeInputName:   "DurationRange",
		RangeOperandType: "Duration",
	},
	// PRD §26.4 "Opaque columns" — the four comparator.Opaque[T]
	// monomorphizations (Rule 1). Operands are typed with the SAME scalar the
	// column's own field declares, so a value a client reads back is exactly
	// the value it can filter on; the text operators are absent because `LIKE`
	// does not exist on `inet` / `macaddr` and matches raw bytes on `bytea`.
	//
	// IPComparator / CIDRComparator / MacAddrComparator are PostgreSQL-only
	// and need no explicit dialect switch to stay that way: a family is
	// emitted only when a column references it (Rule 3, collectComparatorFamilies),
	// and `inet` / `cidr` / `macaddr` exist on no other dialect.
	// BytesComparator is reachable on all three — `bytea` on PostgreSQL, the
	// `blob` family on MySQL and SQLite.
	{
		Name:      "BytesComparator",
		Operators: opaqueComparatorOperators("Bytes"),
	},
	{
		Name:      "IPComparator",
		Operators: opaqueComparatorOperators("IP"),
	},
	{
		Name:      "CIDRComparator",
		Operators: opaqueComparatorOperators("CIDR"),
	},
	{
		Name:      "MacAddrComparator",
		Operators: opaqueComparatorOperators("MacAddr"),
	},
	// PRD §26.4 "Comparator input projection" — the two document families.
	// Operator order matches the struct field order in `comparator/json.go`
	// and `comparator/jsonb.go` so schema and Go read in step.
	//
	// JSONComparator is emitted on PostgreSQL and MySQL: comparator.JSON.Parse
	// switches on the dialect itself (`col::jsonb @>` / `col::jsonb ?` on
	// PostgreSQL, JSON_CONTAINS / JSON_CONTAINS_PATH on MySQL), so the same
	// input serves both. It has no `sqlite` arm, so a JSON column on SQLite is
	// non-filterable and Rule 3 never sees the family — the same structural
	// gate JSONBComparator gets below, one dialect further. The `contains` operand is the `JSON` scalar rather
	// than `String` because the value is a document, not text: typing it
	// `String` would let a client send `"{"` and get a runtime cast error
	// instead of a parse-time rejection.
	//
	// The one thing the shared input does NOT normalize is `hasKey`'s
	// operand: PostgreSQL's `?` takes a bare key, MySQL's
	// JSON_CONTAINS_PATH takes a JSON path (PRD §11.2).
	//
	// JSONBComparator is PostgreSQL-only and, like the three network families
	// above, needs no explicit dialect switch to stay that way. The gate is
	// structural: resolveSimpleComparator returns "JSONB" only under
	// config.DialectPostgres, so no column off PostgreSQL can reference the
	// family, and Rule 3 (collectComparatorFamilies) then declines to declare
	// it. A `jsonb`-spelled column on MySQL / SQLite lands on the JSON arm of
	// that same switch, which is exactly §26.4's "falls back to
	// JSONComparator" rule — one derivation, not two.
	{
		Name: "JSONComparator",
		Operators: []APIComparatorOperator{
			{Name: "contains", Type: "JSON"},
			{Name: "hasKey", Type: "String"},
		},
	},
	{
		Name: "JSONBComparator",
		Operators: []APIComparatorOperator{
			{Name: "hasKey", Type: "String"},
			{Name: "hasAnyKey", Type: "[String!]"},
			{Name: "hasAllKeys", Type: "[String!]"},
			{Name: "contains", Type: "JSON"},
			{Name: "containedBy", Type: "JSON"},
			{Name: "pathExists", Type: "String"},
		},
	},
}

// opaqueComparatorOperators builds the operator set every comparator.Opaque[T]
// monomorphization carries, against the given operand scalar. The set is
// identical across the four families — only the operand type differs — so it
// is built rather than restated, and the order matches the struct field order
// in `comparator/opaque.go` so schema and Go read in step.
func opaqueComparatorOperators(scalar string) []APIComparatorOperator {
	return []APIComparatorOperator{
		{Name: "eq", Type: scalar},
		{Name: "neq", Type: scalar},
		{Name: "gt", Type: scalar},
		{Name: "gte", Type: scalar},
		{Name: "lt", Type: scalar},
		{Name: "lte", Type: scalar},
		{Name: "in", Type: "[" + scalar + "!]"},
		{Name: "nin", Type: "[" + scalar + "!]"},
	}
}

// nullableComparatorInput is the input type name a nullable column
// references for a given base family (PRD §26.4 Rule 2).
func nullableComparatorInput(baseInput string) string {
	return "Nullable" + baseInput
}

// nullableComparatorTwin derives a family's `Nullable<X>Comparator` form:
// the identical operand set plus `isNull`, and no range input — the range
// type is declared once alongside the base form and shared by both.
func nullableComparatorTwin(base APIComparatorFamily) APIComparatorFamily {
	ops := make([]APIComparatorOperator, 0, len(base.Operators)+1)
	ops = append(ops, base.Operators...)
	ops = append(ops, APIComparatorOperator{Name: "isNull", Type: "Boolean"})
	return APIComparatorFamily{
		Name:      nullableComparatorInput(base.Name),
		Operators: ops,
	}
}

// comparatorFamilyByName resolves an input type name to its family
// descriptor, deriving the `Nullable<X>Comparator` twin on demand so both
// forms of a family resolve through one lookup.
func comparatorFamilyByName(name string) (APIComparatorFamily, bool) {
	base := strings.TrimPrefix(name, "Nullable")
	for _, f := range shippedComparatorFamilies {
		if f.Name != base {
			continue
		}
		if base != name {
			return nullableComparatorTwin(f), true
		}
		return f, true
	}
	return APIComparatorFamily{}, false
}

// APIPKArg holds the per-PK-column resolver-side binding metadata. For a
// single-PK table the surrounding APITableContext.PKArgs has one entry; for a
// composite PK it has one entry per column and the schema /
// resolver / seed templates iterate over the slice to emit each arg.
type APIPKArg struct {
	// Name is the SQL column name (e.g. "user_id"). Stable across casings;
	// templates do not surface this directly but it is useful for debugging
	// and for mapping back to the parser column.
	Name string
	// GraphQLName is the schema-side / resolver-arg identifier in the
	// configured FieldCasing (e.g. "userId" in camel casing, "user_id" in
	// snake casing). Used both as the GraphQL arg name (`userId: UUID!`) and
	// as the Go arg identifier on the gqlgen-emitted resolver method (gqlgen
	// uses the GraphQL field name verbatim as the Go arg name).
	GraphQLName string
	// GraphQLType is the bare GraphQL scalar (e.g. "UUID", "Int", "ID"). The
	// schema template appends "!" to express non-null-ness — every PK column
	// is non-null per SQL semantics.
	GraphQLType string
	// GoType is the Go type gqlgen generates for the resolver method's arg.
	// Driven by the GraphQL scalar's default Go binding, NOT by the consumer
	// model's column type. Per PRD §26.4 + §26.4.1: GraphQLType=`Int` → "int"
	// (gqlgen's default Int binding), `ID`/`String` → "string", `Boolean` →
	// "bool", `Float` → "float64". Custom scalars (UUID, Decimal, DateTime,
	// etc.) bind to the consumer's model type via the wrapper-merged
	// gqlgen.yml entry, so GoType matches ModelGoType for those.
	GoType string
	// GoImport is the Go import path the resolver file needs to declare
	// GoType. Empty for stdlib / built-in types ("int", "string", "bool",
	// "float64"); set for third-party PK types like "uuid.UUID"
	// (`github.com/google/uuid`). The orchestrator's pkArgImports collector
	// unions GoImport across every PK column on every table for the file's
	// import block.
	GoImport string
	// ModelGoType is the Go type the consumer model's PK struct field expects
	// (e.g. "int64" for `BIGSERIAL PRIMARY KEY`). When ModelGoType differs
	// from GoType, the `pkConvert` helper emits a Go conversion at the model-
	// call boundary. Same value as GoType when no cast is needed.
	ModelGoType string
	// ModelFieldName is the PascalCase field name on the model's <T>PK struct
	// (e.g. "UserID", "CategoryID"). Used by `pkConvert` to construct
	// `models.<T>PK{<ModelFieldName>: <expr>, …}` literals when the table has
	// a composite PK.
	ModelFieldName string
}

// APIFilterField holds gqlgen→model mapping for one filterable column on a
// per-table filter translator (PRD §26.5.3). Drives one if-block in the
// generated translateXxxFilter function:
//
//	if in.<GoFieldName> != nil { out.<ModelFieldName> = <TranslatorFunc>(in.<GoFieldName>) }
type APIFilterField struct {
	// SQLName is the underlying SQL column (e.g. "user_id"). Carried for
	// diagnostics only — no template reads it — so
	// ValidateAPIFilterCompleteness can name the column a translator entry
	// belongs to when the schema half of the pair is missing (PRD §26.5.3).
	SQLName string
	// GraphQLName is the field's name in the emitted schema (e.g. "userId"
	// or "user_id" under snake_case). It is the key sqlgen uses to tell
	// gqlgen what to call the generated Go field — see APIGoFieldOverrides.
	GraphQLName string
	// GoFieldName is the Go field name on the gqlgen-generated <Table>Filter
	// struct (e.g. "UserID"). sqlgen does not predict gqlgen's spelling: it
	// dictates it, by emitting `models.<T>Filter.fields.<GraphQLName>.
	// fieldName: <GoFieldName>` into the merged gqlgen.yml (PRD §26.5.6).
	// gqlgen's modelgen discards its own templates.ToGo result when that
	// override is present, so this name is what the generated struct carries
	// — including for shapes camelization cannot round-trip, such
	// as an acronym following a digit (`line2_id` → "Line2ID").
	GoFieldName string
	// ModelFieldName is the Go field name on the consumer model's
	// <Table>Filter struct (e.g. "Name"). Sourced from the underlying
	// FilterFieldContext.FieldName so flect-style acronym detection (e.g.
	// "user_id" → "UserID") matches the model-side struct exactly.
	ModelFieldName string
	// TranslatorFunc names the comparator translator function dispatched
	// inside the if-block (e.g. "translateStringComparator").
	TranslatorFunc string
}

// APIFilterRelationship is one relationship member on a `<T>Filter` GraphQL
// input — the projection of a model-side [RelationshipFilterContext] onto the
// API surface (PRD §26.4 "Relationship (as a filter)", §26.5.3 "Relationship
// filters").
//
// It is a single value read by BOTH emitters: the schema template renders `{{
// .GraphQLName }}: {{ .InputTypeName }}` and the filter translator renders
// `out.{{ .ModelFieldName }} = {{ .TranslatorFunc }}(in.{{ .GoFieldName }})`.
// That is the one-field-map invariant applied to the relationship half —
// deriving the two independently is what makes a filter field accepted at parse
// time and dropped at execution time.
type APIFilterRelationship struct {
	// GraphQLName is the field's name in the emitted input (e.g. "orderItems").
	// Derived by graphQLFieldName from the relationship's declared name, the
	// same call mapRelationshipToGraphQL makes for the object type's field, so
	// a relationship is spelled identically on both surfaces.
	GraphQLName string
	// GoFieldName is the Go field name on the gqlgen-generated `<T>Filter`
	// struct. Dictated to gqlgen through APIGoFieldOverrides rather than
	// predicted, exactly as a column filter field's is.
	GoFieldName string
	// ModelFieldName is the Go field name on the consumer model's `<T>Filter`
	// struct — RelationshipFilterContext.FieldName, so the member the
	// translator assigns is the member `ToConditions` compiles.
	ModelFieldName string
	// InputTypeName is the target's filter input (e.g. "OrderItemFilter").
	// An ordinary nested input: no new gqlgen machinery, and a self-
	// referential relationship simply names its own input (§13.5).
	InputTypeName string
	// TranslatorFunc names the target's own per-table filter translator
	// (e.g. "translateOrderItemFilter"). It is emitted for every entity on
	// APIContext.Tables, and a member is only produced when the target is one
	// of them, so the call always resolves.
	TranslatorFunc string
}

// APISortField represents one entry in a per-table sort translator switch
// (PRD §26.5.3). The case clause matches on the GraphQL enum value (a
// string-typed gqlgen enum) and returns the SQL column name driving the
// underlying ORDER BY. Comparing on the raw enum string sidesteps any
// drift between flect's PascalCase and gqlgen's Go-const naming
// conventions when an SQL column contains an acronym.
type APISortField struct {
	// EnumValue is the SCREAMING_SNAKE_CASE GraphQL enum value
	// (e.g. "NAME", "CREATED_AT"). Matches the schema-emitted enum.
	EnumValue string
	// SQLColumn is the SQL column name (e.g. "name", "created_at")
	// returned by the switch case.
	SQLColumn string
}

// APIInputField holds the gqlgen-generated Go field name + the matching
// model input field for one column on a Create/Update input. The resolver
// template uses this to emit translateCreate / translateUpdate input bodies.
//
// The translator emission shape is the cross-product of (SchemaNullable) ×
// (ModelOmittable) × (ModelInnerIsPointer) × (CastTo) × (AddressOf):
//
//	SchemaNullable=false, ModelOmittable=false → out.X = COERCE(in.X)
//	SchemaNullable=false, ModelOmittable=true  → out.X = omittable.Set(COERCE(in.X))
//	SchemaNullable=true,  ModelOmittable=true,  inner=T   → if in.X != nil { out.X = omittable.Set(COERCE(*in.X)) }
//	SchemaNullable=true,  ModelOmittable=true,  inner=*T  → if in.X != nil { out.X = omittable.Set(in.X) }
//
// COERCE is identity by default. CastTo wraps the value in `<CastTo>(…)`
// when the model's Go type differs from gqlgen's emitted type (notably
// `int → int32`/`int64` for sized columns). AddressOf flips the pointer-
// wrapped optional case to `omittable.Set(&in.X)` when gqlgen emits a bare
// slice-typed scalar (json.RawMessage) but the model wraps it in a pointer
// for explicit-null semantics.
//
// The (schema-nullable + model-bare) combination is structurally impossible:
// the column would have to be NULL and yet land on a non-omittable model
// field. SchemaNullable=false + ModelOmittable=true + inner=*T is also
// impossible (NOT NULL column with pointer Go type never arises).
type APIInputField struct {
	// GraphQLName is the field's name in the emitted schema; the key sqlgen
	// uses to dictate the Go field name to gqlgen (see APIGoFieldOverrides).
	GraphQLName    string
	GoFieldName    string // Go field name on the gqlgen-generated input, dictated via models.<T>.fields
	ModelFieldName string // consumer model's input field name (e.g. "Name")
	GoType         string // bare Go type without omittable / pointer wrappers (e.g. "string", "int32")
	// SchemaNullable mirrors the column's `col.Nullable`. True → the GraphQL
	// schema declares the field as nullable, gqlgen emits the input field as
	// `*T` (the translator must nil-check before deref). False → schema is
	// `T!`, gqlgen emits bare `T`, no nil-check required.
	SchemaNullable bool
	// ModelOmittable reports whether the consumer model wraps the field in
	// `omittable.Value[…]`. The wrap drives whether the translator emits a
	// bare assignment or `omittable.Set(…)`. Mirrors `InputFieldContext.Omittable`.
	ModelOmittable bool
	// ModelInnerIsPointer reports whether the model's omittable inner type is
	// a pointer (`omittable.Value[*string]`) vs a value (`omittable.Value[string]`).
	// Pointer-inner means the gqlgen-emitted optional `*T` field threads
	// through unmodified (no deref); value-inner requires a `*` deref.
	ModelInnerIsPointer bool
	// CastTo holds the model's Go type when the value needs a cast wrap to
	// match gqlgen's emitted type. Empty when no cast is needed. The most
	// common case is `int → int32`/`int64` because gqlgen always emits Go
	// `int` for the GraphQL `Int` scalar but sqlgen models size-narrows to
	// the column's native width (Postgres INTEGER → `int32`, BIGINT →
	// `int64`, SMALLINT → `int16`). The same mismatch exists on the float
	// axis — gqlgen binds `Float` → `float64` while a `real` / `float4`
	// column resolves to `float32`. The other case is the
	// PostgreSQL enum-array named-slice bridge. Both are decided by comparing
	// gqlgenInputGoType against the model type — see inputCoercionFor, which
	// errors rather than guessing when the two disagree in any other way.
	CastTo string
	// AddressOf reports whether the gqlgen-emitted nullable field uses bare
	// `T` (rather than `*T`) AND the model wraps `*T` inside `omittable.Value`.
	// True for slice-typed scalars like `json.RawMessage` / `types.JSON` when
	// the model uses a pointer wrapper: gqlgen relies on `nil`-slice for the
	// explicit-null semantics, but the model keeps a dedicated pointer to
	// distinguish "not provided" from "explicit null". The translator emits
	// `omittable.Set(&in.X)` to bridge the shape.
	AddressOf bool
	// PointerCast reports that gqlgen emits `*T1` and the model wants `*T2`
	// with `T1 != T2` — the nullable narrowed-numeric case, where gqlgen binds
	// the GraphQL `Int` / `Float` scalar to Go `int` / `float64` while the
	// model resolves to the column's native width
	// (`omittable.Value[*int32]`, `omittable.Value[*float32]`).
	//
	// Pointers thread through unchanged only when their POINTEE types agree,
	// so this case cannot be expressed as a conversion in expression
	// position: `int32(p)` is illegal and `&int32(*p)` is not addressable.
	// The template emits a three-line deref-convert-readdress block instead.
	// False whenever the value can be coerced inline, which is
	// every other branch.
	PointerCast bool
	// GqlgenIsBareNullable reports whether gqlgen emits bare `T` (not `*T`)
	// for a nullable schema field — true for slice scalars (`json.RawMessage`
	// / `types.JSON`). When true the deref branch must NOT emit `*in.X` (the
	// value is already non-pointer); when the model also stores the bare
	// type, the translator passes `in.X` through directly. AddressOf is the
	// parallel flag for the pointer-wrapped variant of the same case.
	GqlgenIsBareNullable bool
	// GqlgenGoType is the Go type sqlgen DICTATES for this field on the
	// gqlgen-generated input struct, emitted as
	// `models.<Input>.fields.<f>.type`. Empty when gqlgen's own default
	// stands, which is every field except a numeric array.
	//
	// When set, the type it names IS the model's, so inputCoercionFor takes
	// the identity arm and the translator emits a plain assignment.
	GqlgenGoType string
}

// APIUpdateOp holds dispatch metadata for one numeric column's
// _set / _inc / _dec input operators on the Update input.
type APIUpdateOp struct {
	SQLName string // "stock"
	// IncGraphQLName / DecGraphQLName are the schema field names of the
	// paired operators ("stock_inc" / "stock_dec"). They key the fieldName
	// overrides that tell gqlgen to call the generated Go fields IncGoField
	// and DecGoField, which is what lets those names stay a plain literal
	// suffix on SetGoField rather than something gqlgen has to re-derive.
	IncGraphQLName string // "stock_inc"
	DecGraphQLName string // "stock_dec"
	SetGoField     string // "Stock"
	IncGoField     string // "StockInc"
	DecGoField     string // "StockDec"
	IncColumnConst string // "ProductIncrementStock"
}

// APIFieldContext holds one column emitted as a GraphQL field on a type.
type APIFieldContext struct {
	SQLName     string // "user_id"
	GraphQLName string // "userId" or "user_id"
	GraphQLType string // "String!" / "UUID" / "DateTime!"
	GraphQLBare string // "String" / "UUID" — without the "!" suffix
	GoFieldName string // PascalCase Go field name on the FieldOptions struct (e.g. "UserID")
	// GoFieldOverridden reports that GoFieldName came from
	// `column_map.<col>.name` rather than from the naming engine. The bound
	// row type then needs an explicit `models.<T>.fields.<f>.fieldName`
	// entry, because gqlgen binds a bound type's fields by case-insensitive
	// comparison against the name it derives from the GraphQL field and
	// would not find the renamed one.
	GoFieldOverridden bool
	Nullable          bool
	PrimaryKey        bool
	Numeric           bool
	// NumericWidthAnchor is the marshaler anchor this column's model Go type
	// needs before gqlgen can bind it. Zero Name when
	// gqlgen already binds the width, when the column resolves to a non-spec
	// scalar, or when §32.2 dropped it from every API surface. Collected into
	// APIContext.NumericWidthScalars; templates do not read it directly.
	NumericWidthAnchor APINumericWidth
	// Filterable reports whether the column should appear in the
	// `input <T>Filter` block of the generated GraphQL schema. It is read
	// back off the resolved Filter projection rather than set beside it, so
	// the template's `$f.Filterable` gate and `$f.Filter.InputTypeName` are
	// the same fact and a gated-in field can never render an empty type.
	//
	// False for columns whose underlying Go type cannot satisfy the
	// comparator package's `T comparable` constraint (e.g. `json[]` /
	// `jsonb[]`), for a `types.JSON` column on SQLite,
	// for columns whose access role drops the filter surface (§32.2 — access
	// only removes an otherwise-present comparator, never adds one), and for
	// the two comparator shapes with no sound GraphQL projection (see
	// resolveAPIFilterProjection). The first three also drop the column from
	// the model-side filter or leave it unreferenced there; the fourth is
	// GraphQL-only — the Go client keeps the member (PRD §26.12).
	Filterable bool
	// Readable / Writable / Sortable carry the §32.2 API capabilities
	// resolved from the column's access role. They gate the schema object
	// type, the create/update inputs (+ `_inc`/`_dec` operators), the sort
	// enum, and the field-options walker case for this column. All true for
	// public columns, so a public-only config renders byte-identically.
	Readable bool
	Writable bool
	Sortable bool
	// InUpdateInput reports whether the column belongs in the GraphQL
	// `Update<T>Input`. Like InCreateInput it is the single signal the
	// schema template, the input translator, and HasUpdateInput all read.
	//
	// PK columns are always excluded — a PK is the row's identity, addressed
	// through the mutation's PK arguments, never set as an attribute. The
	// tenant column is excluded too when the server owns it (see
	// serverOwnedTenantColumn): the runtime drops it from the SET clause and
	// verify-matches it instead, so offering it would advertise a field that
	// can only return FORBIDDEN or be silently discarded. See PRD §29.4.2.
	InUpdateInput bool
	// InCreateInput reports whether the column belongs in the GraphQL
	// `Create<T>Input`. It is the single signal the schema template, the
	// input translator, and HasCreateInput all read, so those three can
	// never disagree about the input's shape.
	//
	// A PK column is included only when the table's PK strategy is
	// `caller` — i.e. the value is not server-generated and an external
	// client MUST supply it (always the case for a composite PK, which is
	// a tuple of foreign keys). DB-strategy (serial / auto-increment /
	// db-default) and app-strategy (server-minted UUID) PKs stay excluded:
	// their identity is owned by the server and must not leak into the
	// public create input. See PRD §26.4.
	InCreateInput bool
	Description   string
	// Filter is the column's resolved GraphQL filter projection — the one
	// derivation the schema template and the filter translator both read
	// (PRD §26.5.3). Zero value when the column contributes no filter
	// field, which is exactly when Filterable is false.
	Filter APIFilterProjection
	// SortEnumValue is the column's SCREAMING_SNAKE_CASE GraphQL sort-enum
	// value (PRD §8.5). Computed once by screamingSnakeCase and read by
	// both the schema's `<T>SortField` enum and the generated
	// `<table>SortFieldToColumn` switch, so the two spellings cannot drift
	// on a digit-leading column. Empty when the column is not sortable.
	SortEnumValue string
}

// APIRelationshipContext holds one relationship emitted as a GraphQL field.
type APIRelationshipContext struct {
	SQLName     string
	GraphQLName string
	TargetType  string // "Company"
	GraphQLType string // "Company" / "Company!" / "[Review!]!"
	GoFieldName string // PascalCase Go field name on the FieldOptions struct (e.g. "Company", "Reviews")
	// IsList reports whether the relationship is a collection (O2M / M2M).
	// Drives the GraphQL emission shape ([Type!]!) AND the walker emission
	// shape — list relationships wrap into <Target>RelationshipOptions per
	// shared/_field_options.tmpl, single relationships use <Target>FieldOptions
	// directly.
	IsList      bool
	Description string
}

// APIScalarUse names a GraphQL scalar in use in the schema.
type APIScalarUse struct {
	Name       string // "JSON", "DateTime", "UUID", "Decimal", "NullUUID", "NullDecimal", "NullDateTime", "Time"
	GoType     string // qualified Go type, e.g. "uuid.UUID"
	GoImport   string // import path, e.g. "github.com/google/uuid"
	Marshaling string // "method" | "external" | "builtin"
	// NullVariant identifies a Null wrapper scalar paired with a non-null
	// member (UUID/NullUUID, Decimal/NullDecimal, DateTime/NullDateTime —
	// PRD §26.4.1 "Null-wrapper scalar pairing"). Drives the schema-side
	// nullability rule (no `!` suffix) and the marshaler template's choice
	// of body shape (`graphql.Null` on `!Valid`).
	NullVariant bool
	// UnderlyingField is the wrapper struct's field name carrying the
	// payload when Valid is true (e.g. "UUID" for `uuid.NullUUID`,
	// "Decimal" for `decimal.NullDecimal`, "Time" for `types.NullDateTime`).
	// Empty when NullVariant is false. Drives the Unmarshal body shape
	// (`out.<UnderlyingField> = parsed`).
	UnderlyingField string
	// MarshalerPackage is the import path of the package declaring the
	// consumer's `Marshal<Name>` / `Unmarshal<Name>` free functions, taken
	// from `api.graphql.scalars.<Name>.marshaler_package`. Set only for
	// consumer-declared external scalars; empty for every built-in registry
	// entry, whose marshalers sqlgen emits into `<resolver_dir>/scalars_gen.go`.
	//
	// It is what separates the two kinds of category-4 scalar: a non-empty
	// value keeps the scalar out of ExternalScalars (sqlgen emits no body for
	// it) and redirects the gqlgen `models:` discovery anchor from the
	// resolver package to the consumer's.
	MarshalerPackage string
}

// APINumericWidth is one gqlgen marshaler anchor for a Go numeric width that
// gqlgen does not bind to its spec scalar out of the box.
//
// It is deliberately NOT an APIScalarUse: no `scalar <Name>` is declared, the
// GraphQL schema is untouched, and the wire type stays `Int` / `Float`. The
// only artifacts are a `Marshal<Name>` / `Unmarshal<Name>` pair in
// scalars_gen.go and one extra path appended to that scalar's gqlgen
// `models:` list (PRD §26.4.1).
type APINumericWidth struct {
	// Name is the marshaler suffix and the gqlgen discovery anchor's type
	// name — "Int16" for `Marshal<Name>` / `Unmarshal<Name>`.
	Name string
	// GoType is the Go type the marshaler pair is typed against ("int16").
	GoType string
	// Scalar is the spec scalar whose `models:` list this anchor joins,
	// "Int" or "Float".
	Scalar string
	// MarshalerPath is the gqlgen `models:` path this width binds through
	// when gqlgen already ships the marshaler pair (every integer width).
	// Empty means sqlgen emits the pair itself into scalars_gen.go and the
	// anchor resolves against the resolver package instead — true for
	// `float32` alone.
	MarshalerPath string
}

// SqlgenEmitted reports whether sqlgen must emit this width's marshaler pair
// into scalars_gen.go, rather than pointing gqlgen at its own bundled one.
func (w APINumericWidth) SqlgenEmitted() bool { return w.MarshalerPath == "" }

// APIEnumContext describes one schema enum projected onto the GraphQL surface.
// Populated by BuildAPIContext for every enum referenced by an API-exposed
// column. Drives the `enum <Name> { ... }` declaration in shared_gen.graphqls
// and the `<Name>: <modelsPkg>.<GoTypeName>` entry in the gqlgen `models:`
// merge — together they let gqlgen marshal the enum value directly without
// emitting a per-field resolver stub.
type APIEnumContext struct {
	// SQLName and SQLSchema are the enum's identity in the database, carried
	// so a GraphQL type-name collision can be reported against the config key
	// that renames it (`enums.<SQLName>.struct_name` — PRD §4.11). Neither is
	// emitted into the schema.
	SQLName   string
	SQLSchema string
	// FromSet distinguishes a MySQL SET projection from a schema enum, so a
	// GraphQL type-name collision names the right entity category and does not
	// offer an `enums:` override that would not be read (EnumContext.FromSet).
	FromSet bool
	// GraphQLName is the GraphQL enum type name. Identical to GoTypeName so
	// the gqlgen `models:` binding can resolve symmetrically (e.g.
	// "DocumentEntityTypeEnum").
	GraphQLName string
	// GoTypeName is the Go enum type name in the consumer's models package
	// (e.g. "DocumentEntityTypeEnum"). Combined with apiCtx.ModelsPackage to
	// produce the fully-qualified path the gqlgen `models:` merge emits.
	GoTypeName string
	// SliceGoTypeName is the named-slice Go type this enum's list form binds
	// to: a PostgreSQL enum array's sibling ("DocumentEntityTypeEnumSlice"),
	// or a MySQL SET's own column type ("UsersPermissionsSet").
	// Empty only for a schema enum that has neither, i.e. one on a dialect
	// without native enum arrays (MySQL inline ENUMs, SQLite TEXT). The gqlgen
	// `models:` merge binds the list-typed GraphQL field
	// (`[<Enum>!]!` / `[<Enum>!]`) to this slice type so gqlgen avoids
	// emitting a resolver stub for list-shaped columns.
	SliceGoTypeName string
	// Values are the GraphQL enum value identifiers, derived from each value
	// via `gqlEnumIdent` (the same helper `templates/enum.go.tmpl` uses for
	// MarshalGQL / UnmarshalGQL switch cases). Identifier shape:
	//   - dotted   `asset.primary`    → "ASSETPRIMARY"
	//   - snake    `multi_word_value` → "MULTI_WORD_VALUE"
	//   - bare     `spv`              → "SPV"
	// The actual SQL literal (e.g. "asset.primary") is preserved on the wire
	// as the enum's named-type underlying string; MarshalGQL bridges between
	// the GraphQL identifier and the SQL literal at the gqlgen boundary.
	Values []APIEnumValue
}

// APIEnumValue carries one enum value's GraphQL-name and Go-constant-name
// pairing, used by the shared schema template and by gqlgen's enum value
// resolution path. Both names are needed because they may differ (the
// GraphQL identifier excludes the GoTypeName prefix and SCREAMING_SNAKEs the
// remainder, while the Go constant retains the table-prefixed PascalCase
// form).
type APIEnumValue struct {
	// GraphQLName is the SCREAMING_SNAKE_CASE GraphQL enum value (e.g.
	// "ASSETPRIMARY"). Always a valid GraphQL identifier (`[A-Z][A-Z0-9_]*`).
	GraphQLName string
	// GoConstantName is the consumer-package Go constant name (e.g.
	// "DocumentEntityTypeEnumAssetprimary"). gqlgen's models: binding uses
	// the named-type round-trip so this is informational only.
	GoConstantName string
}

// --- Scalar registry ---

// builtInScalarRegistry maps Go binding strings to the GraphQL scalar each
// resolves to and how it must be marshaled (PRD §26.4.1).
type scalarBinding struct {
	Name string
	// Marshaling is the marshaler-discovery mode (builtin / method /
	// external). Drives both the template that emits the Marshal* /
	// Unmarshal* functions and the wrapper merge's `models:` path shape.
	Marshaling string
	// NullVariant marks the Null-wrapper member of a paired scalar
	// (PRD §26.4.1). When true, the column's GraphQL field type is
	// emitted without `!` regardless of column nullability — the wrapper
	// itself encodes nullability via its `Valid bool` field.
	NullVariant bool
	// UnderlyingField names the wrapper struct's payload field (e.g.
	// "UUID" for `uuid.NullUUID`). Empty for non-NullVariant entries.
	UnderlyingField string
	// Nilable declares that the bound Go type is Go-nilable — a slice, map,
	// pointer, interface or channel, including a named type whose underlying
	// type is one of those (`net.IP` is a named `[]byte`).
	//
	// It is the single input to the deref decision in the create/update input
	// translator. gqlgen wraps a nullable field's Go type in a pointer unless
	// the type is already nilable (v0.17.90 `codegen/config/binder.go`,
	// `IsNilable` + `CopyModifiersFromAst`), so a nilable binding arrives at
	// the translator as bare `T` and must NOT be dereferenced. Declaring it
	// here rather than re-deriving it per call site is what keeps the fact in
	// one place — see inputCoercionFor.
	//
	// This mirrors `gotype.nullKind`: a `nullRef` type is exactly a nilable
	// one, and its nullable form is the same type rather than `*T`. The two
	// therefore agree by construction, not coincidence.
	Nilable bool
	// Import is the import path the bound Go type needs in the emitted
	// `graph/scalars_gen.go`. Empty for stdlib-universe or builder-supplied
	// types. Read by scalarImports, so a registry addition carries its own
	// import instead of needing a second hand-maintained switch.
	Import string
	// MarshalerImports lists import paths the emitted Marshal/Unmarshal
	// BODY needs beyond Import (e.g. `encoding/base64` for Bytes). Kept
	// separate because it is a property of the marshaling strategy, not of
	// the bound type.
	MarshalerImports []string
}

// scalarKey identifies a Go type binding the way a resolved column carries it:
// import path plus bare type name. `api.graphql.scalars` spells the same
// binding as a single string ("net/netip.Addr"), so the consumer index is
// built by splitting on the last dot (config.SplitGoType) and matching the
// pair. Keying on the pair rather than the qualified short form keeps two
// same-named types from different packages distinct.
type scalarKey struct {
	Import   string
	TypeName string
}

// typeBindings carries the project-wide naming context a column mapping
// consults to decide a field's GraphQL type and its comparator projection:
// schema enums, consumer-declared custom scalars, and the package alias the
// consumer's models are referenced under. They are bundled because they are
// always threaded together from BuildAPIContext down to
// graphQLTypeForGoType, and the intermediate signatures were already at the
// width GO.md §2 says to extract a struct at.
type typeBindings struct {
	// enums maps a Go enum type name (bare and, on PostgreSQL, its named
	// slice sibling) to the enum it projects onto.
	enums map[string]EnumContext
	// scalars maps a Go type to the consumer scalar declared for it. Values
	// carry Name / Marshaling / MarshalerPackage; GoType and GoImport are
	// filled from the column at lookup time.
	scalars map[scalarKey]APIScalarUse
	// modelsPkg is the Go package alias the consumer's generated models are
	// referenced under from inside the graph package. Sourced from
	// cfg.Output.Package — the same value the orchestrator later mirrors
	// onto APIContext.ModelsPackage and writes as the import alias, so the
	// qualification baked into an enum comparator's descriptor and the
	// import line that resolves it are the one config field, read twice.
	//
	// Only the Enum family needs it: every other comparator's type
	// parameter is a builtin or a stdlib type the graph package names
	// directly, while an enum's is a type in the consumer's package.
	modelsPkg string
}

// buildConsumerScalarIndex inverts `api.graphql.scalars` into the Go-type
// lookup graphQLTypeForGoType needs. Entries without a `go_type` (a bare
// `marshaling: builtin` declaration) bind no column and are skipped.
//
// Keys are visited in sorted order so that two entries declaring the same
// go_type resolve deterministically rather than by map-iteration luck.
func buildConsumerScalarIndex(g *config.GraphQLAPIConfig) map[scalarKey]APIScalarUse {
	if len(g.Scalars) == 0 {
		return nil
	}
	out := make(map[scalarKey]APIScalarUse, len(g.Scalars))
	for _, name := range slices.Sorted(maps.Keys(g.Scalars)) {
		sc := g.Scalars[name]
		if sc.GoType == "" {
			continue
		}
		importPath, typeName := config.SplitGoType(sc.GoType)
		out[scalarKey{Import: importPath, TypeName: typeName}] = APIScalarUse{
			Name:             name,
			Marshaling:       sc.Marshaling,
			MarshalerPackage: sc.MarshalerPackage,
		}
	}
	return out
}

// builtInScalarRegistry is the single description of every Go type sqlgen
// itself can resolve a column to that is not a GraphQL spec built-in. An entry
// states four things at once — the scalar name, how it is marshaled, whether
// the Go type is nilable, and what the emitted marshalers import — so that
// adding a type cannot leave one of the four behind. `TestScalarRegistryParity`
// pins the derived tables (config.BuiltInScalarGoTypes, ReservedScalarNames,
// scalarImports, the scalars template) against it.
//
// A Go type absent from here, from the schema enums, from `api.graphql.scalars`
// and from the spec built-ins has no GraphQL binding, and graphQLTypeForGoType
// rejects it rather than guessing `String`.
var builtInScalarRegistry = map[string]scalarBinding{
	"types.JSON":          {Name: "JSON", Marshaling: config.ScalarMarshalingMethod, Nilable: true},
	"types.DateTime":      {Name: "DateTime", Marshaling: config.ScalarMarshalingMethod},
	"types.NullDateTime":  {Name: "NullDateTime", Marshaling: config.ScalarMarshalingMethod, NullVariant: true, UnderlyingField: "Time"},
	"uuid.UUID":           {Name: "UUID", Marshaling: config.ScalarMarshalingExternal},
	"uuid.NullUUID":       {Name: "NullUUID", Marshaling: config.ScalarMarshalingExternal, NullVariant: true, UnderlyingField: "UUID"},
	"decimal.Decimal":     {Name: "Decimal", Marshaling: config.ScalarMarshalingExternal},
	"decimal.NullDecimal": {Name: "NullDecimal", Marshaling: config.ScalarMarshalingExternal, NullVariant: true, UnderlyingField: "Decimal"},
	"json.RawMessage":     {Name: "JSON", Marshaling: config.ScalarMarshalingExternal, Nilable: true, Import: "encoding/json"},
	"time.Time":           {Name: "Time", Marshaling: config.ScalarMarshalingBuiltin},

	// Five Go types the built-in dialect tables reach. Without a binding each
	// would emit a `String` field in front of a non-string Go field and a
	// create/update translator that does not compile. Wire formats are fixed by PRD §26.4; each round-trips exactly
	// through the stdlib parser named in its marshaler.
	"[]byte":           {Name: "Bytes", Marshaling: config.ScalarMarshalingExternal, Nilable: true, MarshalerImports: []string{"encoding/base64"}},
	"time.Duration":    {Name: "Duration", Marshaling: config.ScalarMarshalingExternal, Import: "time"},
	"net.IP":           {Name: "IP", Marshaling: config.ScalarMarshalingExternal, Nilable: true, Import: "net"},
	"net.IPNet":        {Name: "CIDR", Marshaling: config.ScalarMarshalingExternal, Import: "net"},
	"net.HardwareAddr": {Name: "MacAddr", Marshaling: config.ScalarMarshalingExternal, Nilable: true, Import: "net"},

	// Under `overrides.use_pointers: false` every nullable column of a
	// `gotype.nullSQL` type resolves to one of these wrappers, so without them
	// ordinary nullable text / integer / boolean columns would produce a WRONG
	// SCHEMA (a nullable `integer` advertised as `String`) before the
	// translator failed to compile. §26.4.1 already names the
	// `database/sql.NullX` family under Null-wrapper pairing; these are the
	// entries that sentence always implied.
	//
	// Their non-null partner is a spec built-in (`Int` / `Float` / `String` /
	// `Boolean` / `DateTime`) rather than a second registry entry — the
	// §26.4.1 wire-format invariant still holds (`Int` ↔ JSON number), but
	// this is the first pairing shape where only one member is registered.
	"sql.NullString":  {Name: "NullString", Marshaling: config.ScalarMarshalingExternal, NullVariant: true, UnderlyingField: "String", Import: "database/sql"},
	"sql.NullBool":    {Name: "NullBool", Marshaling: config.ScalarMarshalingExternal, NullVariant: true, UnderlyingField: "Bool", Import: "database/sql"},
	"sql.NullInt16":   {Name: "NullInt16", Marshaling: config.ScalarMarshalingExternal, NullVariant: true, UnderlyingField: "Int16", Import: "database/sql"},
	"sql.NullInt32":   {Name: "NullInt32", Marshaling: config.ScalarMarshalingExternal, NullVariant: true, UnderlyingField: "Int32", Import: "database/sql"},
	"sql.NullInt64":   {Name: "NullInt64", Marshaling: config.ScalarMarshalingExternal, NullVariant: true, UnderlyingField: "Int64", Import: "database/sql"},
	"sql.NullFloat64": {Name: "NullFloat64", Marshaling: config.ScalarMarshalingExternal, NullVariant: true, UnderlyingField: "Float64", Import: "database/sql"},
	"sql.NullTime":    {Name: "NullTime", Marshaling: config.ScalarMarshalingExternal, NullVariant: true, UnderlyingField: "Time", Import: "database/sql"},
}

// --- Builder ---

// collectUsedScalars flattens the per-column scalar registrations into the two
// lists APIContext publishes: every scalar the schema declares, sorted by name
// so emission is deterministic, and the subset sqlgen emits a marshaler body
// for.
//
// ExternalScalars drives scalars_gen.go, so it carries only the category-4
// scalars sqlgen actually emits a body for — the built-in registry's. A
// consumer-declared external scalar is marshaled by functions the consumer
// wrote (MarshalerPackage), and scalars.go.tmpl has no body for it; including
// it here would trip generateAPIScalarFile's emptiness gate and write a
// scalars_gen.go with nothing in it. It stays in UsedScalars, so the schema
// still declares `scalar <Name>`.
func collectUsedScalars(scalarsByName map[string]APIScalarUse) (used, external []APIScalarUse) {
	used = make([]APIScalarUse, 0, len(scalarsByName))
	for _, s := range scalarsByName {
		used = append(used, s)
	}
	slices.SortFunc(used, func(a, b APIScalarUse) int {
		return strings.Compare(a.Name, b.Name)
	})

	external = make([]APIScalarUse, 0)
	for _, s := range used {
		if s.Marshaling == config.ScalarMarshalingExternal && s.MarshalerPackage == "" {
			external = append(external, s)
		}
	}
	return used, external
}

// BuildAPIContext walks the table contexts and produces the APIContext.
// Returns nil when API generation is not enabled. `enums` is the full
// enum-context list from the parsed schema — used to project enum-typed
// columns onto GraphQL enum types and to populate `APIContext.UsedEnums`
// for shared-schema emission and the gqlgen models merge. `sets` is the
// MySQL SET list, projected onto the same enum machinery by
// enumContextsForSets.
func BuildAPIContext(tables []TableContext, views []ViewContext, enums []EnumContext, sets []SetContext, cfg *config.RootConfig) (*APIContext, error) {
	if cfg.API == nil || !cfg.API.Enabled {
		return nil, nil
	}
	if cfg.API.GraphQL == nil || !cfg.API.GraphQL.Enabled {
		return nil, nil
	}
	g := cfg.API.GraphQL

	if err := validateScalarMarshaling(g); err != nil {
		return nil, err
	}

	apiTables := make([]APITableContext, 0, len(tables)+len(views))
	scalarsByName := make(map[string]APIScalarUse)
	translatorsByName := make(map[string]APIComparatorTranslator)
	binds := typeBindings{
		enums:     buildEnumGoTypeLookup(append(slices.Clone(enums), enumContextsForSets(sets)...)),
		scalars:   buildConsumerScalarIndex(g),
		modelsPkg: cfg.Output.Package,
	}
	// usedEnumsByName collects enums actually referenced by API-exposed columns
	// across every table. The shared-schema template and the gqlgen models
	// merge consume the deduplicated, sorted result via APIContext.UsedEnums.
	usedEnumsByName := make(map[string]EnumContext)
	// Resolved before the per-table loop because a relationship's filter member
	// and object-type field are both gated on the TARGET's exposure, and the
	// target may be built after the parent (PRD §26.5.3, §26.10).
	exposedTargets := apiExposedTargets(cfg, tables, views)

	for _, tc := range tables {
		if !tableAPIEnabled(cfg, tc.TableName, tc.Schema) {
			continue
		}
		at, err := buildOneAPITable(cfg, g, tc, scalarsByName, translatorsByName, binds, usedEnumsByName, exposedTargets)
		if err != nil {
			return nil, err
		}
		apiTables = append(apiTables, at)
	}

	// The nested-mutation projection (PRD §26.5.1) gates each edge on its
	// target's API context, so it runs once every table's exists — and before
	// the views join the slice, since a view is never a nested target.
	if err := wireAPINestedMutations(apiTables, tables, cfg); err != nil {
		return nil, err
	}

	// Views join the same slice rather than a parallel one: every
	// downstream template, the field-selection walker, the envelope aliases,
	// the gqlgen `models:` merge, the resolver seeds and both translators
	// already iterate APIContext.Tables, and a second collection would fork all
	// of them. Read-onlyness travels on the entry (IsView + the masked
	// Operations), not on which slice it lives in. PRD §26.4 "Views on the
	// GraphQL surface".
	for _, vc := range views {
		if !viewAPIEnabled(cfg, vc.ViewName, vc.Schema) {
			continue
		}
		av, err := buildOneAPIView(cfg, g, vc, scalarsByName, translatorsByName, binds, usedEnumsByName)
		if err != nil {
			return nil, err
		}
		apiTables = append(apiTables, av)
	}

	slices.SortFunc(apiTables, func(a, b APITableContext) int {
		return strings.Compare(a.StructName, b.StructName)
	})

	used, external := collectUsedScalars(scalarsByName)

	translators := make([]APIComparatorTranslator, 0, len(translatorsByName))
	for _, t := range translatorsByName {
		translators = append(translators, t)
	}
	slices.SortFunc(translators, func(a, b APIComparatorTranslator) int {
		return strings.Compare(a.FuncName, b.FuncName)
	})

	usedEnums := buildUsedEnumsList(usedEnumsByName)

	ctx := &APIContext{
		NumericWidthScalars:   collectNumericWidthScalars(apiTables),
		UUIDParseFunc:         gotype.UUIDIntegrationFor("").ParseFunc,
		Package:               g.Package,
		SchemaDir:             g.SchemaDir,
		ResolverDir:           g.ResolverDir,
		FieldCasing:           g.FieldCasing,
		Tables:                apiTables,
		UsedScalars:           used,
		ExternalScalars:       external,
		UsedEnums:             usedEnums,
		ComparatorFamilies:    collectComparatorFamilies(apiTables),
		ComparatorTranslators: translators,
	}

	// Last, because it needs every derived name at once: the enums, the
	// scalars, the emitted comparator surface and the per-table types all
	// share one GraphQL namespace (PRD §26.4 "GraphQL type name ownership").
	if err := validateGraphQLTypeNames(ctx, g.Scalars); err != nil {
		return nil, err
	}
	return ctx, nil
}

// validateScalarMarshaling requires every custom scalar to declare its
// marshaling — already covered by ValidatePreParse, but defending here keeps
// BuildAPIContext honest when a test invokes it with a hand-built cfg. Sorted
// so a config with several offending entries always reports the same one.
func validateScalarMarshaling(g *config.GraphQLAPIConfig) error {
	for _, name := range slices.Sorted(maps.Keys(g.Scalars)) {
		if g.Scalars[name].Marshaling == "" {
			return fmt.Errorf("api.graphql.scalars.%s: marshaling is required (allowed: builtin, method, external)", name)
		}
	}
	return nil
}

// ParseUUIDFunc returns the function name the emitted UUID / NullUUID
// unmarshalers call, falling back to the standard library's spelling when the
// context carries none.
//
// The templates call this rather than reading UUIDParseFunc, because an
// APIContext built by hand — which several tests do, and which BuildAPIContext
// therefore cannot police — would otherwise render `return (s)`: a call with no
// function name, which is not Go and which nothing downstream would catch,
// since scalars_gen.go is emitted rather than compiled in the gen module. The
// fallback is read from the gotype registry rather than restated here so the
// no-integration-selected answer has one definition (PRD §7.4).
func (c *APIContext) ParseUUIDFunc() string {
	if c.UUIDParseFunc == "" {
		return gotype.UUIDIntegrationFor("").ParseFunc
	}
	return c.UUIDParseFunc
}

// NeedsWidthMarshalers reports whether any resolved numeric width needs a
// marshaler pair sqlgen must emit into scalars_gen.go, as opposed to one
// gqlgen already ships.
func (c *APIContext) NeedsWidthMarshalers() bool {
	for _, w := range c.NumericWidthScalars {
		if w.SqlgenEmitted() {
			return true
		}
	}
	return false
}

// collectNumericWidthScalars deduplicates the per-column marshaler anchors into
// the sorted set scalars_gen.go and the gqlgen `models:` merge both read.
//
// Deduplication is by anchor Name, which carries the Go type — two columns of
// the same width share one marshaler pair, and a width reachable under both
// `Int` and `Float` cannot exist (a Go type belongs to exactly one spec
// numeric scalar).
func collectNumericWidthScalars(tables []APITableContext) []APINumericWidth {
	byName := make(map[string]APINumericWidth)
	for _, t := range tables {
		for _, f := range t.Fields {
			if f.NumericWidthAnchor.Name == "" {
				continue
			}
			byName[f.NumericWidthAnchor.Name] = f.NumericWidthAnchor
		}
	}
	if len(byName) == 0 {
		return nil
	}
	out := make([]APINumericWidth, 0, len(byName))
	for _, w := range byName {
		out = append(out, w)
	}
	slices.SortFunc(out, func(a, b APINumericWidth) int {
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

// unusedScalarWarnings reports every `api.graphql.scalars` entry that no
// API-exposed column resolved to.
//
// A `go_type` that matches nothing — a typo, a stale entry after a column was
// retyped — is not what this is for: that case is a hard error from
// graphQLTypeForGoType: the column has no binding, so generation stops and
// names it.
//
// What remains is the genuinely-unused declaration: a column excluded from the
// API by its §32.2 access role or by `api.enabled: false` registers no scalar,
// and a declaration may legitimately land before the column that will use it.
//
// A warning rather than an error, because "unused" is not always a mistake:
// a column excluded from the API by its §32.2 access role (`hidden` /
// `internal`) or by `api.enabled: false` registers no scalar, and a
// declaration may legitimately land before the column that will use it.
// Entries with no `go_type` (a bare `marshaling: builtin` declaration) bind no
// column by definition and are skipped.
//
// Comparing declared names against UsedScalars is sound because a consumer
// scalar can never share a name with a built-in one — config.ReservedScalarNames
// rejects that — so a name in UsedScalars means this declaration is what put
// it there.
//
// Emitted from Generate only. `sqlgen generate` chains the gqlgen wrapper in
// the same run, and the wrapper builds its own APIContext; warning
// from both would report every entry twice.
func unusedScalarWarnings(cfg *config.RootConfig, apiCtx *APIContext) []string {
	if apiCtx == nil || cfg.API == nil || cfg.API.GraphQL == nil {
		return nil
	}
	declared := cfg.API.GraphQL.Scalars
	if len(declared) == 0 {
		return nil
	}
	used := make(map[string]bool, len(apiCtx.UsedScalars))
	for _, s := range apiCtx.UsedScalars {
		used[s.Name] = true
	}
	var out []string
	for _, name := range slices.Sorted(maps.Keys(declared)) {
		if declared[name].GoType == "" || used[name] {
			continue
		}
		out = append(out, fmt.Sprintf(
			"api.graphql.scalars.%s: no API-exposed column resolves to %q, so `scalar %s` is not declared and the binding has no effect — check the go_type spelling, or disregard if the column is deliberately off the API surface (access: hidden/internal, or api.enabled: false)",
			name, declared[name].GoType, name,
		))
	}
	return out
}

// apiTableConfigKey is the `tables.<key>` a warning about this API table names:
// the schema-qualified key when the config spells it that way, the bare table
// name otherwise.
func apiTableConfigKey(cfg *config.RootConfig, at APITableContext) string {
	if _, ok := cfg.Tables[at.Schema+"."+at.SQLTable]; at.Schema != "" && ok {
		return at.Schema + "." + at.SQLTable
	}
	return at.SQLTable
}

// tableContextFor finds the table context an API table was projected from.
func tableContextFor(tables []TableContext, at APITableContext) (TableContext, bool) {
	for _, tc := range tables {
		if tc.TableName == at.SQLTable && tc.Schema == at.Schema {
			return tc, true
		}
	}
	return TableContext{}, false
}

// apiOverReachKey pairs one API mask key with the client method flag it names
// and the schema fact that explains a missing method.
type apiOverReachKey struct {
	name   string
	mask   func(*config.Operations) *bool
	client func(ResolvedOperations) bool
	// why names the missing schema fact; family is the nested family for the
	// three `…_with_related` keys and empty otherwise.
	why    string
	family string
}

// apiOverReachKeys are the API keys a table's client can lack. The rest —
// get, paginate, connection, create, create_many, update, update_where,
// hard_delete — name methods every generated table has (PRD §4.6).
var apiOverReachKeys = []apiOverReachKey{
	{name: "soft_delete", mask: func(o *config.Operations) *bool { return o.SoftDelete }, client: func(r ResolvedOperations) bool { return r.SoftDelete }, why: "the table has no soft-delete column"},
	{name: "restore", mask: func(o *config.Operations) *bool { return o.Restore }, client: func(r ResolvedOperations) bool { return r.Restore }, why: "the table has no soft-delete column"},
	{name: "upsert", mask: func(o *config.Operations) *bool { return o.Upsert }, client: func(r ResolvedOperations) bool { return r.Upsert }, why: "the table has no conflict target"},
	{name: "create_with_related", mask: func(o *config.Operations) *bool { return o.CreateWithRelated }, client: func(r ResolvedOperations) bool { return r.CreateWithRelated }, family: "create"},
	{name: "update_with_related", mask: func(o *config.Operations) *bool { return o.UpdateWithRelated }, client: func(r ResolvedOperations) bool { return r.UpdateWithRelated }, family: "update"},
	{name: "upsert_with_related", mask: func(o *config.Operations) *bool { return o.UpsertWithRelated }, client: func(r ResolvedOperations) bool { return r.UpsertWithRelated }, family: "upsert"},
}

// apiOverReachWarnings names each explicit per-table `api.operations` key set
// true for a method the table's client does not generate (PRD §4.13, §26.5.1).
// The mask is subtractive, so the key is ignored; the warning keeps that from
// being silent.
//
// It reads the table's resolved operations, which are exactly the
// schema-allowed set (PRD §4.6) — the nested flags included, since
// wireNestedMutations has run — so it answers at resolution level rather than
// re-deriving the schema facts from config. A global mask stays silent: it is
// written once for a whole schema and legitimately exceeds some tables.
func apiOverReachWarnings(cfg *config.RootConfig, tables []TableContext, apiCtx *APIContext) []string {
	if apiCtx == nil {
		return nil
	}
	var out []string
	for _, at := range apiCtx.Tables {
		if at.IsView {
			continue
		}
		tcfg := resolveTableConfig(cfg.Tables, at.Schema, at.SQLTable)
		if tcfg.API == nil || tcfg.API.Operations == nil {
			continue
		}
		tc, ok := tableContextFor(tables, at)
		if !ok {
			continue
		}
		mask := tcfg.API.Operations
		for _, k := range apiOverReachKeys {
			if want := k.mask(mask); want == nil || !*want || k.client(tc.Operations) {
				continue
			}
			out = append(out, fmt.Sprintf(
				"tables.%s.api.operations.%s: the API cannot expose %q because the table's client does not generate it — %s; the key is ignored (the mask is subtractive; PRD §26.5.1)",
				apiTableConfigKey(cfg, at), k.name, k.name, overReachReason(cfg, tc, k),
			))
		}
	}
	return out
}

// overReachReason names the schema fact behind a missing method.
func overReachReason(cfg *config.RootConfig, tc TableContext, k apiOverReachKey) string {
	if k.family == "" {
		return k.why
	}
	switch {
	case !nestedMutationsEnabled(cfg) || !nestedMutationAllowed(cfg.Generation.NestedMutations.Operations, k.family):
		return fmt.Sprintf("generation.nested_mutations does not enable the %s family", k.family)
	case k.family == "upsert" && len(tc.ConflictTargets) == 0:
		return "the table has no conflict target"
	default:
		return "the table has no relationship eligible for a nested mutation (PRD §9.9.4)"
	}
}

// flatUpsertOmittedWarnings names each table whose explicit per-table
// `api.operations` mask asks for `upsert` that the API cannot expose because
// the table emits no `<T>ConflictPK` (PRD §9.5): the flat `upsert<T>`
// mutation hard-codes that constant, so it is omitted (§26.5.1).
//
// Scoped like apiOverReachWarnings: only an explicitly-set per-table value
// warns, since a global mask legitimately over-reaches, being written once for
// tables with differing conflict targets. A table whose client does not
// generate Upsert at all — no conflict target of any kind — is left to that
// warning, so one key is never reported twice.
func flatUpsertOmittedWarnings(cfg *config.RootConfig, tables []TableContext, apiCtx *APIContext) []string {
	if apiCtx == nil {
		return nil
	}
	var out []string
	for _, at := range apiCtx.Tables {
		if at.IsView || at.HasConflictPK {
			continue
		}
		tcfg := resolveTableConfig(cfg.Tables, at.Schema, at.SQLTable)
		if tcfg.API == nil || tcfg.API.Operations == nil || tcfg.API.Operations.Upsert == nil || !*tcfg.API.Operations.Upsert {
			continue
		}
		if tc, ok := tableContextFor(tables, at); !ok || !tc.Operations.Upsert {
			continue
		}
		out = append(out, fmt.Sprintf(
			"tables.%s.api.operations.upsert: the API cannot expose upsert%s because the table emits no %sConflictPK (its primary key has no index the generator can see; PRD §9.5) — the flat mutation is omitted (PRD §26.5.1)",
			apiTableConfigKey(cfg, at), at.StructName, at.StructName,
		))
	}
	return out
}

// buildEnumGoTypeLookup builds a Go-type → EnumContext map covering both the
// bare enum (`DocumentEntityTypeEnum`) and its PostgreSQL array sibling
// (`DocumentEntityTypeEnumSlice`) so the column-level lookup in
// `mapColumnToGraphQL` resolves both shapes in O(1).
// enumContextsForSets projects each MySQL SET onto the EnumContext shape the
// API layer already speaks, so a SET column reaches gqlgen through the exact
// path a PostgreSQL enum array does (PRD §26.4).
//
// The projection is not an analogy — the two are the same shape. An enum array
// is a bare enum (`DocumentEntityTypeEnum`) plus a named slice sibling
// (`DocumentEntityTypeEnumSlice`) carrying MarshalGQL / UnmarshalGQL; a SET is
// a value type (`UsersPermissionsSetValue`) plus a named slice
// (`UsersPermissionsSet`) that is the COLUMN's Go type. So the value type maps
// onto GoTypeName — it is what the GraphQL enum is named after and what each
// list element binds to — and the set type onto SliceGoTypeName. Everything
// downstream (buildEnumGoTypeLookup, UsedEnums, the shared-schema `enum` block,
// the wrapper's two-entry `models:` merge) then works unchanged.
func enumContextsForSets(sets []SetContext) []EnumContext {
	out := make([]EnumContext, 0, len(sets))
	for _, s := range sets {
		out = append(out, EnumContext{
			Name:            s.Name,
			Schema:          s.Schema,
			GoTypeName:      s.ValueGoTypeName,
			SliceGoTypeName: s.GoTypeName,
			Values:          s.Values,
			DocComment:      s.DocComment,
			FromSet:         true,
		})
	}
	return out
}

func buildEnumGoTypeLookup(enums []EnumContext) map[string]EnumContext {
	out := make(map[string]EnumContext, len(enums)*2)
	for _, e := range enums {
		out[e.GoTypeName] = e
		if e.SliceGoTypeName != "" {
			out[e.SliceGoTypeName] = e
		}
	}
	return out
}

// buildUsedEnumsList materializes the deduped APIEnumContext slice that drives
// shared-schema emission + gqlgen models merge. Split out of BuildAPIContext
// to keep the latter under the cyclomatic-complexity cap.
func buildUsedEnumsList(usedEnumsByName map[string]EnumContext) []APIEnumContext {
	out := make([]APIEnumContext, 0, len(usedEnumsByName))
	for _, e := range usedEnumsByName {
		out = append(out, buildAPIEnumContext(e))
	}
	slices.SortFunc(out, func(a, b APIEnumContext) int {
		return strings.Compare(a.GoTypeName, b.GoTypeName)
	})
	return out
}

// buildAPIEnumContext projects an EnumContext onto APIEnumContext, deriving
// each GraphQL enum value identifier via the shared `gqlEnumIdent` helper so
// the schema declaration (here) and the runtime MarshalGQL/UnmarshalGQL wire
// format (`templates/enum.go.tmpl`) cannot diverge for any input shape —
// dotted (`asset.primary` → `ASSETPRIMARY`), snake (`multi_word_value` →
// `MULTI_WORD_VALUE`), or bare (`spv` → `SPV`). The Go constant suffix uses
// `toPascalCase(value)` to match `templates/enum.go.tmpl`'s constant
// declaration. The wire-value (the SQL literal, e.g. "asset.primary") is
// unaffected — gqlgen's models: binding round-trips through MarshalGQL /
// UnmarshalGQL and the SQL literal lands in the database column unchanged.
func buildAPIEnumContext(e EnumContext) APIEnumContext {
	values := make([]APIEnumValue, 0, len(e.Values))
	for _, v := range e.Values {
		values = append(values, APIEnumValue{
			GraphQLName:    gqlEnumIdent(v),
			GoConstantName: e.GoTypeName + toPascalCase(v),
		})
	}
	return APIEnumContext{
		SQLName:         e.Name,
		SQLSchema:       e.Schema,
		FromSet:         e.FromSet,
		GraphQLName:     e.GoTypeName,
		GoTypeName:      e.GoTypeName,
		SliceGoTypeName: e.SliceGoTypeName,
		Values:          values,
	}
}

// validateIncDecNamespace rejects schemas where a numeric non-PK column's
// auto-emitted `_inc` / `_dec` paired field collides with another column on
// the same table. The Update input emits `<col>` / `<col>_inc` / `<col>_dec`
// for every numeric column (PRD §26.5.4); a sibling column literally named
// `<col>_inc` would produce a duplicate input field that gqlgen rejects at
// parse time, so we fail codegen earlier with a precise error. Same rule
// applies to the bare `<col>` field (a sibling named identically would
// already collide regardless of numeric paired-fields, but the check covers
// both paths so the error names the colliding column directly).
//
// Eligibility comes from isIncrementEligible, the same predicate that decides
// what the update input actually emits. Re-deriving it from the Go type alone
// used to make this check fire on foreign keys, which the increment surface
// excludes — an error about an operator pair that was never going to be
// emitted.
//
// It runs on every table: the client always generates Update (PRD §4.6), so
// whether the API keeps an update mutation is the mask's call, and a collision
// is a schema defect either way.
func validateIncDecNamespace(tc TableContext) error {
	colNames := make(map[string]bool, len(tc.Columns))
	for _, c := range tc.Columns {
		colNames[c.Name] = true
	}
	for _, c := range tc.Columns {
		if !isIncrementEligible(c) {
			continue
		}
		if colNames[c.Name+"_inc"] {
			return fmt.Errorf("api: table %q numeric column %q collides with sibling column %q "+
				"(generated update input emits both %q and %q as `_inc` operator for %q)",
				tc.TableName, c.Name, c.Name+"_inc", c.Name+"_inc", c.Name+"_inc", c.Name)
		}
		if colNames[c.Name+"_dec"] {
			return fmt.Errorf("api: table %q numeric column %q collides with sibling column %q "+
				"(generated update input emits both %q and %q as `_dec` operator for %q)",
				tc.TableName, c.Name, c.Name+"_dec", c.Name+"_dec", c.Name+"_dec", c.Name)
		}
	}
	return nil
}

// tableAPIEnabled resolves the per-table API enablement: per-table override
// wins over a global on/off (default on when api is enabled).
func tableAPIEnabled(cfg *config.RootConfig, table, schema string) bool {
	tc := resolveTableConfig(cfg.Tables, schema, table)
	if tc.API != nil && tc.API.Enabled != nil {
		return *tc.API.Enabled
	}
	return true
}

// gqlgenGoTypeFor answers the one question every boundary between gqlgen's
// generated Go and the consumer model has to get right: given the GraphQL
// scalar a column resolved to, what Go type does gqlgen emit for it?
//
// The four spec built-ins have a fixed gqlgen binding (PRD §26.4.1 category 1)
// that is independent of the column's own Go type: `Int` → `int`, `Float` →
// `float64`, `String` / `ID` → `string`, `Boolean` → `bool`. Every other
// scalar — the built-in registry's, a consumer's `api.graphql.scalars` entry,
// a schema enum — is bound to the model's own Go type through the
// wrapper-merged `models:` entry, so gqlgen emits exactly modelGoType and no
// coercion exists to get wrong.
//
// That premise is load-bearing, and it holds only because EVERY non-spec
// scalar gets such an entry — including the ones gqlgen bundles, which sqlgen
// used to leave to gqlgen's own defaults on the grounds that it already bound
// them. It did, but to its own `Model[0]` rather than the column's Go type,
// so `Int64` silently broke here. scalarModelPath pins those too.
//
// Two callers, deliberately: buildAPIPKArgs types the PK resolver argument,
// and inputCoercionFor decides the create/update input coercion. Re-deriving
// the same fact from a hand-maintained list of model Go types drifts one type
// at a time (integer widths, `float32`, every other non-spec binding). Any new
// site that needs the gqlgen-side Go type must call this rather than
// reconstruct it.
func gqlgenGoTypeFor(scalar, modelGoType string) string {
	switch scalar {
	case "Int":
		return "int"
	case "ID", "String":
		return "string"
	case "Boolean":
		return "bool"
	case "Float":
		return "float64"
	}
	return modelGoType
}

// gqlgenInputGoType returns the Go type gqlgen emits for one create/update
// input field, before nullability's pointer wrap.
//
// A list field is the only place the answer is not gqlgenGoTypeFor's directly:
// gqlgen emits `[]<element>` for a GraphQL list, so the scalar binding applies
// to the ELEMENT and the slice wrap is re-applied here. That distinction is
// what separates the two slice-shaped cases the translator has to tell apart —
// a `text[]` column whose model type is the same `[]string` gqlgen emits, and
// a PostgreSQL enum array whose model type is the named `<Enum>Slice` while
// gqlgen emits the bare `[]<Enum>`.
func gqlgenInputGoType(col ColumnContext, scalar, modelGoType string) string {
	if elem := setElemGoType(col); elem != "" {
		// A MySQL SET's model type is the named slice; gqlgen types the
		// generated input field as `[]<element>` for the `[<Enum>!]!` field,
		// exactly as it does for a PostgreSQL enum array. Verified against
		// gqlgen v0.17.90 for both nullabilities — the nullable form is a bare
		// `[]T`, not `*T`, which is also what keeps isNilableGqlgenGoType's
		// `[]` arm from emitting a deref.
		return "[]" + elem
	}
	if !col.IsSlice {
		return gqlgenGoTypeFor(scalar, modelGoType)
	}
	if dictated := dictatedInputGoType(col, scalar, modelGoType); dictated != "" {
		return dictated
	}
	elem := col.SliceElemType
	if elem == "" {
		elem = strings.TrimPrefix(modelGoType, "[]")
	}
	return "[]" + gqlgenGoTypeFor(scalar, elem)
}

// dictatedInputGoType returns the Go type sqlgen DICTATES for this column's
// generated create/update input field via gqlgen's
// `models.<Input>.fields.<f>.type`, or "" when gqlgen's own default stands.
//
// Only numeric arrays are dictated, and only when the two sides disagree.
// gqlgen types a generated list field as `[]Model[0]` — `[]int` for `[Int!]!`,
// `[]float64` for `[Float!]!` — against a model that carries the column's
// native width (`[]int32` for `integer[]`). Go has no conversion between
// slices of differing element types, so no translator expression bridges it;
// dictating the field's type removes the disagreement at its source and the
// identity arm of inputCoercionFor applies.
//
// The SCALAR case is deliberately not dictated. gqlgen emits `int` / `float64`
// there and the integer / `float32` width cast is a sound, already-tested
// bridge; dictating it too would churn every numeric input field in every
// consumer's schema to buy nothing.
func dictatedInputGoType(col ColumnContext, scalar, modelGoType string) string {
	// The `[]` check is not redundant with IsSlice: a PostgreSQL enum array's
	// model type is the NAMED slice (`<Enum>Slice`), which carries its own
	// MarshalGQL / UnmarshalGQL and takes the named-slice cast arm instead.
	if !col.IsSlice || !strings.HasPrefix(modelGoType, "[]") {
		return ""
	}
	// Only the two spec numerics can disagree at all — every other scalar
	// binds gqlgen to the model's own Go type through the `models:` entry.
	if _, ok := gqlgenBoundNumericGoTypes[scalar]; !ok {
		return ""
	}
	elem := strings.TrimPrefix(modelGoType, "[]")
	if gqlgenGoTypeFor(scalar, elem) == elem {
		return ""
	}
	return modelGoType
}

// gqlgenBoundNumericGoTypes lists the Go types gqlgen ships a marshaler for
// under each spec numeric scalar — the exhaustive set a BOUND model field may
// carry without falling off gqlgen's binder.
//
// gqlgen v0.17.90 `codegen/config/config.go::injectBuiltins` registers `Int` as
// `graphql.Int` / `Int32` / `Int64` and `Float` as `graphql.FloatContext`
// alone, and `internal/code/compare.go::CompatibleTypes` demands an EXACT
// basic-kind match. A row-struct field of any other width therefore matches no
// entry, `Binder.TypeReference` returns an error, and `codegen/field.go::
// buildField` swallows it into `f.IsResolver = true` — a silent
// `panic("not implemented")` field resolver, outside the reach of the
// wrapper's Query/Mutation stub rewriter.
//
// This is a DIFFERENT question from gqlgenNumericWidthCasts below, which is
// about the type gqlgen emits for a GENERATED position (input object field,
// resolver argument) — there `Model[0]` always wins and the width is bridged
// by a cast. Both facts are needed and neither implies the other.
var gqlgenBoundNumericGoTypes = map[string][]string{
	"Int":   {"int", "int32", "int64"},
	"Float": {"float64"},
}

// numericWidthAnchor returns the marshaler anchor a column's model Go type
// needs before gqlgen can bind it to `scalar`. The zero value means none is
// needed — gqlgen already binds the width, the scalar is not a spec numeric,
// or §32.2 dropped the column from every API surface.
//
// The wrapper appends the anchor to that scalar's `models:` list. gqlgen's own
// entries stay first in the list, so every GENERATED position keeps binding
// `Model[0]` (`int` / `float64`) and the input width casts are undisturbed.
//
// apiVisible is a parameter rather than the caller's guard so the whole
// decision reads as one expression at the call site.
func numericWidthAnchor(scalar, goType string, apiVisible bool) APINumericWidth {
	if !apiVisible {
		return APINumericWidth{}
	}
	bound, ok := gqlgenBoundNumericGoTypes[scalar]
	if !ok || slices.Contains(bound, goType) {
		return APINumericWidth{}
	}
	w, ok := numericWidthAnchors[goType]
	if !ok {
		return APINumericWidth{}
	}
	w.Scalar = scalar
	return w
}

// numericWidthAnchors describes every Go numeric width sqlgen can resolve a
// column to that gqlgen does not bind.
//
// Every integer width resolves to a marshaler gqlgen ALREADY SHIPS —
// `graphql/int.go` carries MarshalInt8 / MarshalInt16 and `graphql/uint.go`
// the whole unsigned family, complete with NumberOverflowError / UintSignError
// range checking. They are simply absent from injectBuiltins' `Int` model
// list, so nothing ever reaches them. Naming them in the list is the entire
// fix: sqlgen emits no code for these and inherits gqlgen's own overflow
// behaviour rather than reimplementing it.
//
// `float32` is the sole exception — `graphql/float.go` binds `float64` only —
// so sqlgen emits that one pair into scalars_gen.go and anchors it there.
var numericWidthAnchors = map[string]APINumericWidth{
	"int8":    {Name: "Int8", GoType: "int8", MarshalerPath: gqlgenGraphQLPkg + ".Int8"},
	"int16":   {Name: "Int16", GoType: "int16", MarshalerPath: gqlgenGraphQLPkg + ".Int16"},
	"uint":    {Name: "Uint", GoType: "uint", MarshalerPath: gqlgenGraphQLPkg + ".Uint"},
	"uint8":   {Name: "Uint8", GoType: "uint8", MarshalerPath: gqlgenGraphQLPkg + ".Uint8"},
	"uint16":  {Name: "Uint16", GoType: "uint16", MarshalerPath: gqlgenGraphQLPkg + ".Uint16"},
	"uint32":  {Name: "Uint32", GoType: "uint32", MarshalerPath: gqlgenGraphQLPkg + ".Uint32"},
	"uint64":  {Name: "Uint64", GoType: "uint64", MarshalerPath: gqlgenGraphQLPkg + ".Uint64"},
	"float32": {Name: "Float32", GoType: "float32"},
}

// gqlgenGraphQLPkg is the import path of gqlgen's runtime graphql package,
// where its bundled Marshal* / Unmarshal* functions live. Aliased from config,
// which needs the same path to pin the bundled-scalar bindings, so
// the two tables cannot drift onto different packages.
const gqlgenGraphQLPkg = config.GqlgenGraphQLPkg

// gqlgenNumericWidthCasts lists the (gqlgen Go type → model Go type) pairs a
// plain Go conversion is the intended bridge for. It is the ONLY licensed
// disagreement between the two sides.
//
// gqlgen binds each numeric GraphQL scalar to one Go type while sqlgen models
// resolve to the column's native width: PostgreSQL INTEGER → `int32`, BIGINT →
// `int64`, SMALLINT → `int16`, MySQL TINYINT → `int8` plus the unsigned
// variants on the integer axis; `real` / `float4` / MySQL `float` → `float32`
// on the float axis. `int` and `float64` are gqlgen's own bindings and need
// nothing.
//
// The `int` → `uint*` rows are a width conversion, not a value-preserving one:
// a negative `Int` wraps. That is inherent to modelling an unsigned column as
// GraphQL `Int` (which is signed 32-bit) and predates this table; it is listed
// here so the licence is explicit rather than implied by a predicate's name.
var gqlgenNumericWidthCasts = map[string][]string{
	"int":     {"int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64"},
	"float64": {"float32"},
}

// isNumericWidthCast reports whether a plain `model(gqlgenValue)` conversion is
// the declared bridge between the two types.
func isNumericWidthCast(gqlgenGoType, modelGoType string) bool {
	return slices.Contains(gqlgenNumericWidthCasts[gqlgenGoType], modelGoType)
}

// isNilableGqlgenGoType reports whether gqlgen leaves a nullable field's Go
// type unwrapped because the type already carries a nil state.
//
// Mirrors gqlgen v0.17.90 `codegen/config/binder.go`'s `IsNilable` for the
// shapes sqlgen can produce: the syntactic containers, plus the named types
// the built-in registry declares `Nilable` (`net.IP` and `net.HardwareAddr`
// are named `[]byte`; `types.JSON` is a named `json.RawMessage`).
//
// Known limit: a consumer `api.graphql.scalars` entry bound to a NAMED nilable
// type sqlgen does not ship is invisible here — nothing in a Go type string
// reveals that `example.com/x.Thing` is a slice underneath. Such a column
// would take the deref arm and fail to compile, exactly as it does today; a
// consumer-declarable nilability flag would be a new config field and is
// deliberately not invented here (PRD §26.4.1 has no such field).
func isNilableGqlgenGoType(goType string) bool {
	switch {
	case strings.HasPrefix(goType, "[]"),
		strings.HasPrefix(goType, "map["),
		strings.HasPrefix(goType, "*"):
		return true
	}
	return builtInScalarRegistry[goType].Nilable
}

// entityKindTable and entityKindView are the two APIEntity kinds. They are the
// noun every API diagnostic phrases itself in, and the key readOnly() tests, so
// they are constants rather than two string literals a typo could silently
// divide.
const (
	entityKindTable = "table"
	entityKindView  = "view"
)

// APIEntity is the narrow, entity-kind-agnostic view of a schema entity that
// the per-column API resolution pass and the walker-completeness lint read.
//
// It exists because a view IS a table on the read half — same columns, same
// filter fields, same optional PK — and every API surface derived from that
// half must be derived once, for both kinds. Passing a TableContext (or worse,
// a synthetic one assembled from a ViewContext) would make the view path read
// table-only fields as zero values, which is the silent-divergence failure mode
// this type exists to remove.
//
// The Kind / Name pair is carried so every error and lint message names the
// entity the consumer wrote, in the words they wrote it in ("view
// product_summary", not "table product_summary").
type APIEntity struct {
	// Kind is "table" or "view" — the noun every diagnostic uses.
	Kind string
	// Name is the SQL relation name (`products`, `product_summary`).
	Name string
	// Schema is the SQL schema, empty in single-schema projects.
	Schema string
	// StructName is the resolved Go / GraphQL type name (`Product`).
	StructName string
	// Dialect drives comparator resolution — the schema's filter input and the
	// model's comparator type are chosen from one dialect, never two.
	Dialect config.Dialect
	// Columns is the entity's full column set, in declaration order.
	Columns []ColumnContext
	// PKColumns is the resolved primary key: a table's declared PK, a view's
	// `@pk` annotation (or a matview's qualifying unique index). Empty when the
	// entity has none, which is legal for a view (§16.4).
	PKColumns []ColumnContext
	// FilterFields is the model-side filter struct's field list, the iteration
	// order the generated filter translator follows.
	FilterFields []FilterFieldContext
	// Relationships is empty for views — §16.4 gives them none — which is what
	// makes a view's field-selection walker columns-only and its read exactly
	// one query under the §25.1 contract.
	Relationships []RelationshipContext
	// RelationshipFilters is the model-side `<T>Filter` relationship member
	// list — the set APITableContext.FilterRelationships is a subset of, after
	// the §26.5.3 target-exposure gate. Carried so
	// ValidateAPIFilterCompleteness can check that every emitted member names
	// a member the model struct actually has. Always empty for a view.
	RelationshipFilters []RelationshipFilterContext
	// Incrementable names the columns eligible for `_inc` / `_dec` operators.
	// Always empty for a view: the operators drive an Update mutation, and a
	// view has no mutation half.
	Incrementable map[string]bool
	// CursorKeys is the entity's resolved cursor-key column list (PRD §4.13).
	// Read only by rowIdentityFields, as the fallback for a view with no `@pk`
	// — a table always has a PK, and a view's cursor keys are the closest thing
	// it has to one. Empty for a view whose resolved keys do not exist on its
	// projection, which is also what suppresses its Connection.
	CursorKeys []string
}

// apiEntityFromTable and apiEntityFromView are the only two constructors. Both
// project onto the same shape so the shared builders below cannot tell the
// kinds apart except where they deliberately look at Kind.
func apiEntityFromTable(tc TableContext) APIEntity {
	return APIEntity{
		Kind:                entityKindTable,
		Name:                tc.TableName,
		Schema:              tc.Schema,
		StructName:          tc.StructName,
		Dialect:             tc.Dialect,
		Columns:             tc.Columns,
		PKColumns:           tc.PKColumns,
		FilterFields:        tc.FilterFields,
		Relationships:       tc.Relationships,
		RelationshipFilters: tc.RelationshipFilters,
		Incrementable:       incrementableColumnSet(tc),
		CursorKeys:          tc.CursorKeys,
	}
}

func apiEntityFromView(vc ViewContext) APIEntity {
	return APIEntity{
		Kind:       entityKindView,
		Name:       vc.ViewName,
		Schema:     vc.Schema,
		StructName: vc.StructName,
		Dialect:    vc.Dialect,
		Columns:    vc.Columns,
		PKColumns:  vc.PKColumns,
		// A view's model-side filter struct is built by the same
		// buildFilterFields the table path uses (context_view.go), so the
		// generated GraphQL filter translator iterates in the same order.
		FilterFields: vc.FilterFields,
		// Left nil on all three counts, deliberately and not for lack of a
		// source: ViewContext.Relationships and ViewContext.RelationshipFilters
		// are both documented as always empty, and a view has no Update
		// mutation for `_inc` / `_dec` to ride.
		Relationships:       nil,
		RelationshipFilters: nil,
		Incrementable:       nil,
		CursorKeys:          vc.CursorKeys,
	}
}

// readOnly reports whether this entity has a write half at all. It is the one
// place the kind is interrogated, so "a view is read-only" is stated once
// instead of re-derived at each site that needs it.
func (e APIEntity) readOnly() bool { return e.Kind == entityKindView }

// rowIdentityFields resolves the FieldOptions fields the generated walker sets
// when a selection set asks for the row but names no column — `__typename`
// alone, an edge selecting only its `cursor`, a `pageInfo` sub-selection.
// Something has to be projected, or PRD §9.6's all-false
// short-circuit — which means "the caller wants no rows" — swallows a row the
// request did ask for.
//
// The PK is the answer wherever there is one: it is the smallest identity a row
// has, and every column a consumer *derives* from the row (cursor keys,
// relationship mapping keys, O2O NULL-detection keys) is already unioned in by
// the client method that reads it (§9.6 "Structurally required columns"), so
// naming those here would state one requirement in two places. The fallbacks
// exist for a view, the one entity kind that may have no `@pk` (§16.4): its
// resolved cursor keys, then its first column.
func rowIdentityFields(ent APIEntity) []string {
	if len(ent.PKColumns) > 0 {
		out := make([]string, 0, len(ent.PKColumns))
		for _, col := range ent.PKColumns {
			out = append(out, col.FieldName)
		}
		return out
	}
	if len(ent.CursorKeys) > 0 {
		fieldByColumn := make(map[string]string, len(ent.Columns))
		for _, col := range ent.Columns {
			fieldByColumn[col.Name] = col.FieldName
		}
		out := make([]string, 0, len(ent.CursorKeys))
		for _, key := range ent.CursorKeys {
			if field, ok := fieldByColumn[key]; ok {
				out = append(out, field)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if len(ent.Columns) > 0 {
		return []string{ent.Columns[0].FieldName}
	}
	return nil
}

// apiEntityDescription resolves the type-level doc block for one entity: its
// own comment when it has one, else the generated placeholder naming the SQL
// relation and its kind.
//
// The fallback used to live in schema.graphqls.tmpl with "table" hard-coded in
// it, which would have described every view as a table the moment views joined
// APIContext.Tables.
func apiEntityDescription(description, structName, sqlName, kind string) string {
	if description != "" {
		return description
	}
	return structName + " corresponds to the " + sqlName + " " + kind + "."
}

// buildOneAPITable builds a single table's API context, running the
// codegen-time guards that must hold before and after the build: the
// `_inc` / `_dec` namespace check on the source table, then the walker-,
// filter- and sort-completeness lints on the result.
func buildOneAPITable(cfg *config.RootConfig, g *config.GraphQLAPIConfig, tc TableContext,
	scalarsByName map[string]APIScalarUse, translatorsByName map[string]APIComparatorTranslator,
	binds typeBindings, usedEnumsByName map[string]EnumContext, exposedTargets map[string]bool,
) (APITableContext, error) {
	if err := validateIncDecNamespace(tc); err != nil {
		return APITableContext{}, err
	}
	// §26.5.1: the API surface exposes the table's operations masked by
	// `api.operations` (per-table, else global). Subtractive only — the
	// resolver delegates to the Go client, so an operation the client does
	// not generate can never be exposed.
	apiOps, err := resolveAPIOperations(cfg, tc)
	if err != nil {
		return APITableContext{}, err
	}
	at, err := buildAPITableContext(tc, apiOps, g, cfg.Output.Package, scalarsByName, translatorsByName, binds, usedEnumsByName, exposedTargets)
	if err != nil {
		return APITableContext{}, err
	}
	// Walker-completeness lint (PRD §26.5.2): every readable column and every
	// relationship into an exposed entity MUST appear in the generated walker,
	// and nothing else may. The API table context is the input the walker
	// template iterates over, so the check pairs the source TableContext with
	// the just-built APITableContext. Any divergence here would silently
	// produce nil entries on the FieldOptions tree at runtime — failing fast
	// at codegen is the sole guard.
	ent := apiEntityFromTable(tc)
	if err := ValidateAPIWalkerCompleteness(ent, at, exposedTargets); err != nil {
		return APITableContext{}, err
	}
	// Filter- and sort-completeness lints (PRD §26.5.3): the same
	// fail-fast contract as the walker lint, applied to the two surfaces whose
	// halves are emitted by separate templates. Run on the built context, so
	// they check what the templates will actually iterate rather than what the
	// builder intended.
	if err := ValidateAPIFilterCompleteness(ent, at); err != nil {
		return APITableContext{}, err
	}
	if err := ValidateAPISortCompleteness(ent, at); err != nil {
		return APITableContext{}, err
	}
	return at, nil
}

// viewReadOperations is a view's base API operation set before any mask: the
// read operations §26.4 closes the surface to, each gated on the view
// actually generating the client method behind it.
//
// Get needs a resolved primary key — an `@pk` annotation, or a matview's
// qualifying unique index (§16.5.2). Connection needs `cursor_keys` to resolve
// against the view's projection; a view has no PK fallback (§4.13), and
// HasConnection is the same decision the Go client's Connection method is
// emitted from, read here rather than re-derived.
//
// Every mutation stays false by construction, not by masking: a view has no
// mutation half at all (§16.4), so there is nothing for a mask to subtract.
func viewReadOperations(vc ViewContext) ResolvedOperations {
	return ResolvedOperations{
		Get:        vc.HasPK,
		Paginate:   true,
		Connection: vc.HasConnection,
	}
}

// resolveViewAPIOperations intersects a view's read set with its resolved
// `api.operations` mask. Subtractive only, exactly as on tables: maskAPIOperations
// ANDs each field, so a mask naming a mutation cannot add one — and
// config.validateViewAPIOperations rejects the attempt before it reaches here,
// so the block cannot be a silent no-op either.
func resolveViewAPIOperations(cfg *config.RootConfig, vc ViewContext) (ResolvedOperations, error) {
	viewCfg, _ := resolveViewConfig(cfg.Views, vc.Schema, vc.ViewName)
	mask, err := config.ResolveViewAPIOperationsMask(viewCfg, cfg.API)
	if err != nil {
		return ResolvedOperations{}, fmt.Errorf("api operations for view %s: %w", vc.ViewName, err)
	}
	return maskAPIOperations(viewReadOperations(vc), mask), nil
}

// viewAPIEnabled reports whether a view is exposed on the GraphQL surface.
// The twin of tableAPIEnabled, differing only in reading `views.<n>.api`.
//
// It resolves through config.ResolveViewAPIEnabled rather than defaulting to
// true the way tableAPIEnabled does, because BuildAPIContext has already
// established that `api.enabled` is true by the time either is called — so the
// two agree, and the view helper additionally states the inheritance the PRD
// spells out (§4.9 tri-state: nil inherits global).
func viewAPIEnabled(cfg *config.RootConfig, view, schema string) bool {
	vc, _ := resolveViewConfig(cfg.Views, schema, view)
	return config.ResolveViewAPIEnabled(vc, cfg.API)
}

// buildOneAPIView is buildOneAPITable's read-only twin. It runs the same
// walker-, filter- and sort-completeness lints against the same APIEntity
// shape, so a view can never reach emission with a walker, filter input or
// sort enum that disagrees with its schema type (PRD §26.5.2, §26.5.3), and
// then the read-only guard a table has no need of.
func buildOneAPIView(cfg *config.RootConfig, g *config.GraphQLAPIConfig, vc ViewContext,
	scalarsByName map[string]APIScalarUse, translatorsByName map[string]APIComparatorTranslator,
	binds typeBindings, usedEnumsByName map[string]EnumContext,
) (APITableContext, error) {
	apiOps, err := resolveViewAPIOperations(cfg, vc)
	if err != nil {
		return APITableContext{}, err
	}
	av, err := buildAPIViewContext(vc, apiOps, g, scalarsByName, translatorsByName, binds, usedEnumsByName)
	if err != nil {
		return APITableContext{}, err
	}
	ent := apiEntityFromView(vc)
	if err := ValidateAPIWalkerCompleteness(ent, av, nil); err != nil {
		return APITableContext{}, err
	}
	// A view's read surface is filtered and sorted by the same two templates a
	// table's is, so it is linted by the same two functions — no view arm
	// (PRD §16.4).
	if err := ValidateAPIFilterCompleteness(ent, av); err != nil {
		return APITableContext{}, err
	}
	if err := ValidateAPISortCompleteness(ent, av); err != nil {
		return APITableContext{}, err
	}
	// The read-only guard runs on the built context, not on the source view, so
	// it checks what the templates will actually iterate (PRD §16.4 / §26.4).
	if err := ValidateAPIViewReadOnly(av); err != nil {
		return APITableContext{}, err
	}
	return av, nil
}

// buildAPIViewContext produces the per-view APITableContext and accumulates
// scalar uses into scalarsByName plus comparator translators into
// translatorsByName — the same two side-effect maps the table path fills, so a
// scalar or comparator family a view is the only user of still reaches
// shared_gen.graphqls and comparator_translate_gen.go.
//
// Everything on the write half is left at its zero value on purpose, and each
// one is load-bearing rather than incidental:
//
//   - HasCreateInput / HasUpdateInput false suppress `Create<V>Input` /
//     `Update<V>Input` and every mutation that consumes them through the
//     existing empty-input gates.
//   - Relationships nil makes the field-selection walker columns-only, which is
//     what makes a view read exactly one query under the §25.1 contract.
//   - UpdateOps / IncrementEnumType empty keep the `_inc` / `_dec` operators
//     off a surface that has no Update mutation to carry them.
//   - HasConflictPK false keeps upsert unreachable even if a mask named it.
//
// PRD §26.4 "Views on the GraphQL surface".
func buildAPIViewContext(vc ViewContext, apiOps ResolvedOperations, g *config.GraphQLAPIConfig,
	scalarsByName map[string]APIScalarUse, translatorsByName map[string]APIComparatorTranslator,
	binds typeBindings, usedEnumsByName map[string]EnumContext,
) (APITableContext, error) {
	ent := apiEntityFromView(vc)
	usedInView := make(map[string]bool)

	// callerPK and tenantCol drive the create/update input membership flags,
	// which buildAPIColumnFields skips outright for a read-only entity, so both
	// are passed at their neutral values rather than resolved from the view.
	cols, err := buildAPIColumnFields(ent, g, false, "", scalarsByName, usedInView, binds, usedEnumsByName)
	if err != nil {
		return APITableContext{}, err
	}
	fields := cols.fields

	pkArgs, err := buildAPIPKArgs(ent, g.FieldCasing, scalarsByName, usedInView, binds, usedEnumsByName)
	if err != nil {
		return APITableContext{}, fmt.Errorf("view %s: %w", vc.ViewName, err)
	}
	pkType := "ID"
	if len(pkArgs) >= 1 {
		pkType = pkArgs[0].GraphQLType
	}

	apiFilterFields := buildAPIFilterFields(ent, fields, translatorsByName)
	sortFields, sortEnumType, sortInputType := buildAPISortBindings(ent, fields)

	queryName := toCamelCase(vc.StructName)

	return APITableContext{
		StructName:       vc.StructName,
		SnakeName:        vc.SnakeName,
		StructNamePlural: StructNamePlural(vc.StructName),
		QueryName:        queryName,
		QueryNamePlural:  toCamelCase(StructNamePlural(vc.StructName)),
		ListQueryName:    queryName + "List",
		SQLTable:         vc.ViewName,
		Schema:           vc.Schema,
		Description:      apiEntityDescription(vc.Description, vc.StructName, vc.ViewName, ent.Kind),
		APIEnabled:       true,
		IsView:           true,
		ClientAccessor:   entityAccessorName(vc.StructName, true),
		Operations:       apiOps,
		HasSoftDelete:    false,
		HasConflictPK:    false,
		PKType:           pkType,
		PKArgs:           pkArgs,
		Fields:           fields,
		Relationships:    nil,
		HasNumericColumn: cols.hasNumeric,
		UsedScalars:      sortedKeys(usedInView),
		HasCreateInput:   false,
		HasUpdateInput:   false,
		FilterFields:     apiFilterFields,
		SortEnumGoType:   sortEnumType,
		SortInputType:    sortInputType,
		SortFields:       sortFields,

		RowIdentityFields: rowIdentityFields(ent),
	}, nil
}

// apiIncrementColumns returns the columns the API may offer `_inc` / `_dec`
// operators for: the model-side increment set, or nothing when the table's
// operation set excludes Increment.
//
// The operators dispatch to the client's generated `Increment` method and the
// `<T>IncrementColumn` enum (`api/resolvers.go.tmpl`), and both are emitted
// only under `.Operations.Increment` (`table/increment.go.tmpl`,
// `table/client.go.tmpl`). Deriving the API surface from the column set alone
// therefore emitted a resolver calling a method that was never generated —
// `operations.increment: false` plus an enabled API did not compile.
func apiIncrementColumns(tc TableContext) []ColumnContext {
	if !tc.Operations.Increment {
		return nil
	}
	return tc.IncrementColumns
}

// incrementableColumnSet names the columns eligible for `_inc` / `_dec`
// operators. Eligibility is decided once on the model side
// (buildIncrementColumns / isIncrementEligible) and read here rather than
// re-derived from the Go type: re-deriving would put the operators on any
// int-typed column, including the identifying ones the model deliberately
// excludes — primary keys, and foreign keys, where `col + 1` is arithmetic on
// another row's identity.
func incrementableColumnSet(tc TableContext) map[string]bool {
	columns := apiIncrementColumns(tc)
	out := make(map[string]bool, len(columns))
	for _, c := range columns {
		out[c.Name] = true
	}
	return out
}

// apiInputEmission reports whether any surviving mutation would reference the
// create / update input types. The flat upsert carries the same
// HasConflictPK requirement as its mutation gate, and the nested upsert
// wraps the create input too, so a table reachable only through upsert
// without a target either can use does not emit the create input.
// nestedUpsert must be the API-side answer: the Go client's nested upsert
// with at least one API-writable conflict target left.
//
// The answer is settled before buildAPINested projects the edges, so one
// shape still leaves an orphan `Create<T>Input` (accepted as
// harmless: gqlgen allows an unused input, and it builds): a table with no
// PK target, an upsert-only mask and an API-writable unique target, whose
// every nested edge the projection drops. narrowTo then drops the
// nested upsert after this input was kept for it.
func apiInputEmission(apiOps ResolvedOperations, hasConflictPK, nestedUpsert bool) (create, update bool) {
	return apiOps.Create || apiOps.CreateMany || (apiOps.Upsert && hasConflictPK) ||
			(apiOps.UpsertWithRelated && nestedUpsert),
		apiOps.Update || apiOps.UpdateWhere
}

func resolveAPIOperations(cfg *config.RootConfig, tc TableContext) (ResolvedOperations, error) {
	mask, err := config.ResolveAPIOperationsMask(
		resolveTableConfig(cfg.Tables, tc.Schema, tc.TableName), cfg.API,
	)
	if err != nil {
		return ResolvedOperations{}, fmt.Errorf("api operations for %s: %w", tc.TableName, err)
	}
	return maskAPIOperations(tc.Operations, mask), nil
}

// maskAPIOperations intersects a table's resolved operations with the
// `api.operations` mask (PRD §26.5.1), yielding the operations the generated
// API surface may expose. A nil mask passes the operations through unchanged.
//
// Intersection — never union — is the invariant: every generated resolver
// delegates to the Go client, so the API can only ever offer a subset of what
// the client generates. It CAN offer less, including for reads: masking
// `get`, `paginate` or `connection` hides the query while the client method
// stays available to hand-written resolvers. That asymmetry is the whole
// point of the feature.
//
// Every key names the client method its surface calls: `<T>List` is backed by
// Paginate and masked through `paginate`, `update<T>s(filter, input)` by
// UpdateWhere and masked through `update_where`. Operations with no API
// projection (exists, count, increment, stream, upsert_many) are untouched;
// the config layer rejects any attempt to name them in a mask block.
//
// GetMany and UpdateMany are the two exceptions, and they fail closed: a set
// mask turns both off. They are the client methods whose keys once gated
// `<T>List` and `update<T>s`, so an API reader left on either would keep a
// query or mutation on a table whose mask took `paginate` or `update_where`
// away. Zeroed, such a reader can only drop surface the mask kept, which is
// loud in the mask tests rather than a silently open door.
//
// Each of the three nested-mutation keys is conjoined with the masked base
// operation it composes, so masking `create` off the API takes
// `create<T>WithRelated` with it (PRD §26.5.1). Without that, a mask written
// to leave a single, hand-guarded door into a table would leave a second,
// generated one through the nested mutation.
func maskAPIOperations(ops ResolvedOperations, mask *config.Operations) ResolvedOperations {
	if mask == nil {
		return ops
	}
	ops.Get = ops.Get && deref(mask.Get)
	ops.GetMany = false
	ops.Paginate = ops.Paginate && deref(mask.Paginate)
	ops.Connection = ops.Connection && deref(mask.Connection)
	ops.Create = ops.Create && deref(mask.Create)
	ops.CreateMany = ops.CreateMany && deref(mask.CreateMany)
	ops.Update = ops.Update && deref(mask.Update)
	ops.UpdateWhere = ops.UpdateWhere && deref(mask.UpdateWhere)
	ops.UpdateMany = false
	ops.Upsert = ops.Upsert && deref(mask.Upsert)
	ops.SoftDelete = ops.SoftDelete && deref(mask.SoftDelete)
	ops.HardDelete = ops.HardDelete && deref(mask.HardDelete)
	ops.Restore = ops.Restore && deref(mask.Restore)
	return maskNestedAPIOperations(ops, mask)
}

// maskNestedAPIOperations masks the three nested-mutation toggles, each
// conjoined with the already-masked base operation it composes.
func maskNestedAPIOperations(ops ResolvedOperations, mask *config.Operations) ResolvedOperations {
	ops.CreateWithRelated = ops.CreateWithRelated && deref(mask.CreateWithRelated) && ops.Create
	ops.UpdateWithRelated = ops.UpdateWithRelated && deref(mask.UpdateWithRelated) && ops.Update
	ops.UpsertWithRelated = ops.UpsertWithRelated && deref(mask.UpsertWithRelated) && ops.Upsert
	return ops
}

// buildAPITableContext produces the per-table APITableContext and accumulates
// scalar uses into scalarsByName plus comparator translators into
// translatorsByName.
func buildAPITableContext(tc TableContext, apiOps ResolvedOperations, g *config.GraphQLAPIConfig, modelsPkg string, scalarsByName map[string]APIScalarUse, translatorsByName map[string]APIComparatorTranslator, binds typeBindings, usedEnumsByName map[string]EnumContext, exposedTargets map[string]bool) (APITableContext, error) {
	queryName := toCamelCase(tc.StructName)
	queryNamePlural := toCamelCase(StructNamePlural(tc.StructName))
	listQueryName := queryName + "List"

	usedInTable := make(map[string]bool)
	ent := apiEntityFromTable(tc)

	// A caller-strategy PK is supplied by the client, not the server, so its
	// columns are part of the GraphQL create input (PRD §26.4). Composite PKs
	// are always caller-strategy — they are a tuple of foreign keys.
	callerPK := tc.PKStrategy == config.PKStrategyCaller
	// The tenant column the update surface must omit (PRD §29.4.2), or "".
	tenantCol := serverOwnedTenantColumn(tc)

	cols, err := buildAPIColumnFields(ent, g, callerPK, tenantCol, scalarsByName, usedInTable, binds, usedEnumsByName)
	if err != nil {
		return APITableContext{}, err
	}
	fields := cols.fields

	// A relationship into an entity the API hides has no type to name, so it
	// has no field either (PRD §26.10). The Go model keeps the edge.
	rels := make([]APIRelationshipContext, 0, len(tc.Relationships))
	for _, r := range tc.Relationships {
		if !exposedTargets[r.TargetStructName] {
			continue
		}
		rels = append(rels, mapRelationshipToGraphQL(r, g.FieldCasing))
	}

	pkArgs, err := buildAPIPKArgs(ent, g.FieldCasing, scalarsByName, usedInTable, binds, usedEnumsByName)
	if err != nil {
		return APITableContext{}, fmt.Errorf("table %s: %w", tc.TableName, err)
	}
	pkType := "ID"
	if len(pkArgs) >= 1 {
		pkType = pkArgs[0].GraphQLType
	}

	hasConflictPK := tableHasConflictPK(tc)
	usedInTableNames := sortedKeys(usedInTable)

	// The nested upsert survives on the API only while a target is left after
	// the §32.3 access projection, the same test buildAPINested
	// drops it on, so the create input is not kept for a mutation that
	// target test removes. The later edge projection can still remove it; see
	// apiInputEmission for that accepted orphan.
	nestedUpsertTargets, _ := apiNestedConflictTargets(&tc)
	emitsCreateInput, emitsUpdateInput := apiInputEmission(apiOps, hasConflictPK,
		tc.Nested != nil && tc.Nested.EmitUpsert && len(nestedUpsertTargets) > 0)

	gqlNameByColumn := graphQLNamesByColumn(fields)

	createInputFields, updateInputFields, err := buildAPIMutationInputs(tc, gqlNameByColumn, cols.scalarByColumn, modelsPkg, callerPK, tenantCol)
	if err != nil {
		return APITableContext{}, err
	}

	updateOps, incEnumType := buildAPIIncrementBindings(tc, gqlNameByColumn)
	apiFilterFields := buildAPIFilterFields(ent, fields, translatorsByName)
	apiFilterRels := buildAPIFilterRelationships(tc.RelationshipFilters, g.FieldCasing, exposedTargets, filterFieldNameSet(apiFilterFields))
	sortFields, sortEnumType, sortInputType := buildAPISortBindings(ent, fields)

	return APITableContext{
		StructName:          tc.StructName,
		SnakeName:           tc.SnakeName,
		StructNamePlural:    StructNamePlural(tc.StructName),
		QueryName:           queryName,
		QueryNamePlural:     queryNamePlural,
		ListQueryName:       listQueryName,
		SQLTable:            tc.TableName,
		Schema:              tc.Schema,
		Description:         apiEntityDescription(tc.Description, tc.StructName, tc.TableName, ent.Kind),
		APIEnabled:          true,
		IsView:              false,
		ClientAccessor:      entityAccessorName(tc.StructName, false),
		Operations:          apiOps,
		HasSoftDelete:       tc.SoftDelete != nil,
		HasConflictPK:       hasConflictPK,
		PKType:              pkType,
		PKArgs:              pkArgs,
		Fields:              fields,
		Relationships:       rels,
		HasNumericColumn:    cols.hasNumeric,
		UsedScalars:         usedInTableNames,
		IncrementEnumType:   incEnumType,
		HasIncrementColumns: incEnumType != "",
		UpdateOps:           updateOps,
		CreateInputFields:   createInputFields,
		UpdateInputFields:   updateInputFields,
		// Empty-input rule: an input type needs ≥1 field, because
		// gqlgen rejects an empty `input X {}` block. Both flags count the
		// fields their own template block actually emits, so the schema and
		// the flag can't disagree.
		//
		// Create counts InCreateInput — which includes caller-strategy PKs,
		// so an all-PK junction (a pure M2M link table) now emits an input
		// holding exactly its FK tuple instead of being suppressed. A
		// genuinely empty input (a lone server-generated surrogate PK with
		// no other column, or §32.2 access dropping every writable column)
		// still yields false and omits the input type with its mutations.
		//
		// Update counts InUpdateInput, which drops every PK (a PK is the
		// row's identity, addressed through the mutation's PK args, not a
		// settable attribute) and the server-owned tenant column. An all-PK
		// table has nothing to update and keeps emitting nothing; so does a
		// table whose only writable non-PK column is its tenant column.
		//
		// Both also require at least one mutation that CONSUMES the input to
		// be exposed (§26.5.1). Without that an `api.operations` mask — or a
		// read-only table — would emit an input type no mutation references,
		// which gqlgen turns into a dead Go model. The input-translate
		// template already gated on the operations; folding the check in here
		// makes the schema template agree instead of duplicating the
		// expression at each site.
		HasCreateInput:      cols.createInputCount > 0 && emitsCreateInput,
		HasUpdateInput:      cols.updateInputCount > 0 && emitsUpdateInput,
		FilterFields:        apiFilterFields,
		FilterRelationships: apiFilterRels,
		SortEnumGoType:      sortEnumType,
		SortInputType:       sortInputType,
		SortFields:          sortFields,

		RowIdentityFields: rowIdentityFields(ent),
	}, nil
}

// apiColumnFields is the per-column pass's result: the field contexts plus the
// three tallies and the scalar index derived alongside them in one walk.
type apiColumnFields struct {
	fields []APIFieldContext
	// scalarByColumn records the GraphQL type each field resolved to, so the
	// create/update input coercion is decided against this very resolution
	// rather than a second, independent one.
	scalarByColumn   map[string]string
	hasNumeric       bool
	createInputCount int
	updateInputCount int
}

// buildAPIColumnFields runs the per-column mapping for one entity. Extracted
// from buildAPITableContext so the resolution pass and the assembly of the
// entity context read as separate units, and taking an APIEntity so a view
// runs the identical pass — the per-column GraphQL projection is the half a
// view shares with a table in full.
func buildAPIColumnFields(ent APIEntity, g *config.GraphQLAPIConfig, callerPK bool, tenantCol string,
	scalarsByName map[string]APIScalarUse, usedInTable map[string]bool,
	binds typeBindings, usedEnumsByName map[string]EnumContext,
) (apiColumnFields, error) {
	out := apiColumnFields{
		fields:         make([]APIFieldContext, 0, len(ent.Columns)),
		scalarByColumn: make(map[string]string, len(ent.Columns)),
	}
	incrementable := ent.Incrementable

	for _, col := range ent.Columns {
		mapped, elemScalar, err := mapColumnToGraphQL(col, g.FieldCasing, ent.Dialect, scalarsByName, usedInTable, binds, usedEnumsByName)
		if err != nil {
			return apiColumnFields{}, fmt.Errorf("%s %s: %w", ent.Kind, ent.Name, err)
		}
		out.scalarByColumn[col.Name] = elemScalar
		if incrementable[col.Name] {
			mapped.Numeric = true
			out.hasNumeric = true
		}
		// A read-only entity has no mutation inputs for a column to be a member
		// of (§16.4), so both flags stay false rather than being computed and
		// then ignored downstream. An APIFieldContext claiming membership in a
		// create input that no create input exists for is exactly the kind of
		// inert context field to avoid — the next surface to read it (a REST
		// projection, the relationship filters) would have no way
		// to know the claim was inert.
		if !ent.readOnly() {
			mapped.InCreateInput = apiFieldInCreateInput(col, mapped.Writable, callerPK)
			mapped.InUpdateInput = apiFieldInUpdateInput(col, mapped.Writable, tenantCol)
		}
		out.fields = append(out.fields, mapped)
		if mapped.InCreateInput {
			out.createInputCount++
		}
		if mapped.InUpdateInput {
			out.updateInputCount++
		}
	}
	return out, nil
}

// buildAPIMutationInputs builds both mutation input field lists for a table.
// Paired because they share every input and differ only in the two flags, and
// because either failing is the same class of failure — a column whose Go type
// the translator cannot bridge onto gqlgen's.
func buildAPIMutationInputs(tc TableContext, gqlNameByColumn, scalarByColumn map[string]string, modelsPkg string, callerPK bool, tenantCol string) (create, update []APIInputField, err error) {
	create, err = buildAPIInputFields(tc.CreateInputFields, tc.Columns, gqlNameByColumn, scalarByColumn, modelsPkg, callerPK, "")
	if err != nil {
		return nil, nil, fmt.Errorf("table %s create input: %w", tc.TableName, err)
	}
	update, err = buildAPIInputFields(tc.UpdateInputFields, tc.Columns, gqlNameByColumn, scalarByColumn, modelsPkg, false, tenantCol)
	if err != nil {
		return nil, nil, fmt.Errorf("table %s update input: %w", tc.TableName, err)
	}
	return create, update, nil
}

// buildAPIPKArgs builds the per-PK-column resolver-side binding metadata that
// every per-PK schema field, resolver method signature, and seed delegation
// iterates over. For a single-PK table the slice has one entry; for a
// composite-PK table the slice has one entry per column.
//
// scalarsByName / usedInTable accumulate scalar registrations side-effect-
// style — the PK column's GraphQL type may resolve to a custom scalar (UUID,
// Decimal, DateTime, …) that must be tracked alongside the rest of the
// table's columns.
//
// Per PRD §26.4 + §26.4.1 the resolver-side Go type comes from the GraphQL
// scalar's gqlgen-default binding, NOT the model column type — so an `Int!`
// PK on `bigint` (Go `int64`) declares `id int` on the resolver method and
// the body casts `int64(id)` at the model-call boundary via `pkConvert`.
// Custom scalars bind to the model type directly via the wrapper-merged
// gqlgen.yml entry, so GoType matches ModelGoType for those.
func buildAPIPKArgs(ent APIEntity, casing string, scalarsByName map[string]APIScalarUse, usedInTable map[string]bool, binds typeBindings, usedEnumsByName map[string]EnumContext) ([]APIPKArg, error) {
	if len(ent.PKColumns) == 0 {
		return nil, nil
	}
	out := make([]APIPKArg, 0, len(ent.PKColumns))
	for _, col := range ent.PKColumns {
		mapped, bareScalar, err := mapColumnToGraphQL(col, casing, ent.Dialect, scalarsByName, usedInTable, binds, usedEnumsByName)
		if err != nil {
			return nil, err
		}
		// Same `IsSlice` gate as mapColumnToGraphQL: a PK is never an array
		// column, but stripping `[]` unconditionally would mis-type a
		// `[]byte` PK arg as `byte`.
		modelGoType := strings.TrimPrefix(col.GoType, "*")
		argGoType := gqlgenGoTypeFor(bareScalar, modelGoType)
		// PK arg matches the model's Go type (custom scalars: UUID, Decimal,
		// DateTime, etc.) — third-party imports flow through. The four spec
		// built-ins (int / string / bool / float64) take the gqlgen default
		// binding and don't require an import.
		argImport := ""
		if argGoType == modelGoType {
			argImport = col.Import
		}
		out = append(out, APIPKArg{
			Name:           col.Name,
			GraphQLName:    mapped.GraphQLName,
			GraphQLType:    bareScalar,
			GoType:         argGoType,
			GoImport:       argImport,
			ModelGoType:    modelGoType,
			ModelFieldName: col.FieldName,
		})
	}
	return out, nil
}

// tableHasConflictPK reports whether the table emits the
// `<T>ConflictPK` constant the flat upsert resolver passes to Upsert. It reads
// the conflict targets the Go client emits rather than the PK columns, because
// buildConflictTargets withholds the PK target from a key with no index
// behind it (PRD §9.5) — the PK columns alone would promise a constant that
// does not exist.
func tableHasConflictPK(tc TableContext) bool {
	pkConst := tc.StructName + "ConflictPK"
	return slices.ContainsFunc(tc.ConflictTargets, func(ct ConflictTargetContext) bool {
		return ct.ConstantName == pkConst
	})
}

// sortedKeys returns a set's members in a deterministic order, so emission
// driven by it does not churn between runs.
func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// graphQLNamesByColumn indexes the emitted GraphQL field name by SQL column.
//
// sqlgen dictates the Go spelling of each of these fields to gqlgen
// (APIGoFieldOverrides), so the GraphQL name is the join key between the two
// generators and must be derived exactly once — every builder that needs it
// reads this map rather than re-running graphQLFieldName.
func graphQLNamesByColumn(fields []APIFieldContext) map[string]string {
	out := make(map[string]string, len(fields))
	for _, f := range fields {
		out[f.SQLName] = f.GraphQLName
	}
	return out
}

// buildAPIIncrementBindings collects the per-table increment columns and
// derives the IncrementColumn enum type name. Empty when the table has no
// numeric non-PK columns.
func buildAPIIncrementBindings(tc TableContext, gqlNameByColumn map[string]string) (ops []APIUpdateOp, enumType string) {
	ops = buildUpdateOps(tc, gqlNameByColumn)
	if len(ops) > 0 {
		enumType = tc.StructName + "IncrementColumn"
	}
	return ops, enumType
}

// buildAPISortBindings derives the per-table sort field set and the matching
// gqlgen-emitted Go type names. Empty when the table has no sortable columns.
//
// gqlgen v0.17.x mirrors the GraphQL input type name verbatim, so the schema's
// `input <Table>Sort { ... }` produces Go `<Table>Sort` (no "Input" suffix).
func buildAPISortBindings(ent APIEntity, apiFields []APIFieldContext) (fields []APISortField, enumType, inputType string) {
	fields = buildAPISortFields(apiFields)
	if len(fields) > 0 {
		enumType = ent.StructName + "SortField"
		inputType = ent.StructName + "Sort"
	}
	return fields, enumType, inputType
}

// buildAPIFilterFields produces one APIFilterField per non-PK column that
// carries a filter projection with a translator (PRD §26.5.3). Side-effect:
// each unique comparator translator referenced is registered into
// translatorsByName so the project-wide comparator_translate_gen.go covers
// every variant the per-table filters dispatch into.
//
// The per-column decision was made once in resolveAPIFilterProjection; this
// function only reads it. A column whose projection carries no translator
// contributes no entry — and, with no StringComparator fallback, such a
// column carries no input type either, so the schema's
// `$f.Filterable` gate skips it in the same breath. The two surfaces cannot
// drift because there is only one decision behind both, and
// ValidateAPIFilterCompleteness (PRD §26.5.3) is the guard that says so from
// the outside.
func buildAPIFilterFields(ent APIEntity, fields []APIFieldContext, translatorsByName map[string]APIComparatorTranslator) []APIFilterField {
	pkColNames := make(map[string]bool, len(ent.PKColumns))
	for _, c := range ent.PKColumns {
		pkColNames[c.Name] = true
	}

	projByColumn := make(map[string]APIFilterProjection, len(fields))
	gqlNameByColumn := make(map[string]string, len(fields))
	for _, f := range fields {
		projByColumn[f.SQLName] = f.Filter
		gqlNameByColumn[f.SQLName] = f.GraphQLName
	}

	// Iteration follows the entity's FilterFields (sorted by model field name)
	// so the generated translator body keeps the model filter's field order.
	out := make([]APIFilterField, 0, len(ent.FilterFields))
	for _, ff := range ent.FilterFields {
		if pkColNames[ff.ColumnName] {
			continue
		}
		proj := projByColumn[ff.ColumnName]
		if proj.TranslatorFunc == "" {
			continue
		}
		// A nullable variant is self-contained: it takes its own
		// Nullable<X>Comparator input, so it can no longer forward that
		// value to the base translator (PRD §26.4 Rule 2 makes the two
		// distinct GraphQL input types). Registering the base alongside it
		// would emit a function nothing calls.
		translatorsByName[proj.Translator.FuncName] = proj.Translator
		out = append(out, APIFilterField{
			SQLName:        ff.ColumnName,
			GraphQLName:    gqlNameByColumn[ff.ColumnName],
			GoFieldName:    ff.FieldName,
			ModelFieldName: ff.FieldName,
			TranslatorFunc: proj.TranslatorFunc,
		})
	}
	return out
}

// buildAPISortFields produces one APISortField per API-sortable column on
// the table, reading the enum value straight off the field context so the
// generated `<table>SortFieldToColumn` switch matches the schema-emitted
// `<T>SortField` enum character for character (a digit-leading column like
// `2010_revenue` emits COL_2010_REVENUE on both sides). Iteration
// follows the source column order so the enum and the switch align. Never
// empty for a generated table: PK columns are validated to stay `public` /
// `read_only` (§32.4), both of which keep the sort surface.
func buildAPISortFields(fields []APIFieldContext) []APISortField {
	out := make([]APISortField, 0, len(fields))
	for _, f := range fields {
		if !f.Sortable {
			continue
		}
		out = append(out, APISortField{
			EnumValue: f.SortEnumValue,
			SQLColumn: f.SQLName,
		})
	}
	return out
}

// comparatorTranslatorVariant inspects a comparator type expression
// (e.g. `*comparator.NullableNumber[int32]`) and returns the matching
// translator descriptor. The second return is false when the comparator type
// has no GraphQL projection — an Enum whose type parameter is not a schema
// enum, and a Slice whose element operand shape sqlgen has not measured, fall
// outside the supported set. Its caller turns that into a zero projection, so
// the column is dropped from the GraphQL filter input as well as from the
// translator (PRD §26.5.3, §26.12).
//
// `enumBind` is the schema enum the column's Go type resolves to, or the zero
// value when it resolves to none, and `elemGraphQL` is an array column's
// ELEMENT GraphQL type. Both are threaded in because neither the GraphQL enum
// name, the models-qualified Go type a monomorphization spells, nor the
// operand type a list is declared with is recoverable from the type expression
// alone.
func comparatorTranslatorVariant(comparatorType string, enumBind enumComparatorBinding, elemGraphQL string) (APIComparatorTranslator, bool) {
	t := strings.TrimPrefix(comparatorType, "*")
	t = strings.TrimPrefix(t, "comparator.")
	nullable := strings.HasPrefix(t, "Nullable")
	t = strings.TrimPrefix(t, "Nullable")

	var typeParam string
	if i := strings.Index(t, "["); i >= 0 {
		typeParam = strings.TrimSuffix(t[i+1:], "]")
		t = t[:i]
	}

	if typeParam != "" {
		return genericComparatorTranslator(t, typeParam, enumBind, elemGraphQL, nullable)
	}
	return plainComparatorTranslator(t, nullable)
}

// plainComparatorTranslator resolves the families whose comparator type
// carries NO type parameter. Each is a single Go struct, so the descriptor is
// fully determined by the family name and the column's nullability.
func plainComparatorTranslator(family string, nullable bool) (APIComparatorTranslator, bool) {
	switch family {
	case "String":
		return makeNonNumericTranslator("String", "StringComparator", nullable), true
	case "Bool":
		return makeNonNumericTranslator("Bool", "BooleanComparator", nullable), true
	case "Time":
		return makeNonNumericTranslator("Time", "TimeComparator", nullable), true
	case "ID":
		return makeNonNumericTranslator("ID", "IDComparator", nullable), true
	case "JSON":
		return makeNonNumericTranslator("JSON", "JSONComparator", nullable), true
	case "JSONB":
		return makeNonNumericTranslator("JSONB", "JSONBComparator", nullable), true
	}
	return APIComparatorTranslator{}, false
}

// genericComparatorTranslator resolves the four families parameterized on a Go
// type. Each is monomorphized per concrete parameter (PRD §26.4 Rule 1), and
// each may decline: an unmeasured Opaque type, a non-schema enum, or an array
// element with no measured operand shape drops its column off the GraphQL
// filter surface entirely rather than advertising operands the generated body
// cannot copy.
//
// The split from plainComparatorTranslator mirrors the one resolveComparatorType
// already draws between resolveGenericComparator and resolveSimpleComparator,
// and for the same reason: the two halves answer different questions, and only
// this one needs the bindings the type expression cannot spell.
func genericComparatorTranslator(family, typeParam string, enumBind enumComparatorBinding, elemGraphQL string, nullable bool) (APIComparatorTranslator, bool) {
	switch family {
	case "Number":
		if typeParam == durationGoType {
			// PRD §26.4 "Duration columns". The model comparator is
			// Number[time.Duration] like any other numeric, but the operand
			// scalar is `Duration`, not `Float` — so the family splits out
			// under Rule 1's operand-type keying. The generated body is the
			// ordinary Number arm: gqlgen types every `Duration` operand as
			// `time.Duration` already, so the arm's `T(...)` conversions are
			// identities and the Range construction is unchanged.
			return makeDurationTranslator(nullable), true
		}
		return makeNumericTranslator(typeParam, nullable), true
	case "Opaque":
		return makeOpaqueTranslator(typeParam, nullable)
	case "Enum":
		return makeEnumTranslator(typeParam, enumBind, nullable)
	case "Slice":
		if elemGraphQL == "" {
			return APIComparatorTranslator{}, false
		}
		return makeSliceTranslator(typeParam, enumBind, elemGraphQL, nullable)
	}
	return APIComparatorTranslator{}, false
}

// durationGoType is the one Number[T] instantiation that does not collapse
// onto the `Float` scalar (PRD §26.4 Rule 1).
const durationGoType = "time.Duration"

// opaqueComparatorBindings describes the four comparator.Opaque[T]
// monomorphizations: the GraphQL scalar each binds to, and the two shapes
// gqlgen gives that scalar's operands inside a comparator input.
//
// Both booleans are MEASURED against gqlgen v0.17.90, not inferred. The scalar
// operand is pointer-wrapped exactly when the Go type is not nilable
// (`codegen/config/binder.go`, `IsNilable` + `CopyModifiersFromAst`) — the same
// rule isNilableGqlgenGoType already encodes. The LIST element rule is
// different and cannot be derived from the same flag: gqlgen wraps a non-null
// list element in a pointer when the Go type is a STRUCT, so `[CIDR!]` becomes
// `[]*net.IPNet` while `[Duration!]` becomes bare `[]time.Duration` even though
// neither type is nilable. Within this family the two happen to coincide
// because net.IPNet is the only non-nilable member and it is a struct;
// time.Duration is the counterexample that stops the coincidence being a rule,
// which is why the table states both facts rather than deriving one.
// TestOpaqueComparatorBindings_matchRegistryAndPredicate pins the scalar names
// against builtInScalarRegistry.
var opaqueComparatorBindings = map[string]struct {
	Scalar     string
	OperandPtr bool
	ElemPtr    bool
}{
	"[]byte":           {Scalar: "Bytes", OperandPtr: false, ElemPtr: false},
	"net.IP":           {Scalar: "IP", OperandPtr: false, ElemPtr: false},
	"net.IPNet":        {Scalar: "CIDR", OperandPtr: true, ElemPtr: true},
	"net.HardwareAddr": {Scalar: "MacAddr", OperandPtr: false, ElemPtr: false},
}

// makeOpaqueTranslator builds the translator descriptor for one
// comparator.Opaque[T] monomorphization (PRD §26.4 Rule 1, §26.5.3). The
// second return is false for a T with no binding, which keeps an unknown
// opaque type non-filterable on both sides rather than emitting a schema
// field the translator cannot serve.
func makeOpaqueTranslator(typeParam string, nullable bool) (APIComparatorTranslator, bool) {
	bind, ok := opaqueComparatorBindings[typeParam]
	if !ok {
		return APIComparatorTranslator{}, false
	}
	inputType := bind.Scalar + "Comparator"
	tr := APIComparatorTranslator{
		FuncName:          "translate" + inputType,
		InputTypeName:     inputType,
		Family:            "Opaque",
		Nullable:          false,
		OpaqueT:           typeParam,
		OperandIsPointer:  bind.OperandPtr,
		ListElemIsPointer: bind.ElemPtr,
		GoReturnType:      "*comparator.Opaque[" + typeParam + "]",
		OutStructName:     "comparator.Opaque[" + typeParam + "]",
	}
	if !nullable {
		return tr, true
	}
	tr.FuncName = "translateNullable" + inputType
	tr.InputTypeName = nullableComparatorInput(inputType)
	tr.Nullable = true
	tr.GoReturnType = "*comparator.NullableOpaque[" + typeParam + "]"
	tr.OutStructName = "comparator.NullableOpaque[" + typeParam + "]"
	return tr, true
}

// makeEnumTranslator builds the translator descriptor for one
// comparator.Enum[T] monomorphization (PRD §26.4 Rule 1, §26.5.3). The second
// return is false when the column's comparator type parameter is not a schema
// enum, which keeps such a column on the pre-existing StringComparator
// fallback rather than advertising an input whose operands name a GraphQL
// enum the schema never declares.
//
// That guard is load-bearing, not defensive: resolveComparatorType routes
// every unqualified PascalCase Go type through comparator.Enum[T]
// (isEnumLikeType), so a composite-type or domain-type column arrives here
// too and must not be projected as an enum.
//
// Like the Opaque translators, the body needs no conversion — the GraphQL
// enum is bound to the consumer's Go enum type by the gqlgen `models:` merge
// (PRD §26.4.1), so gqlgen already hands the operands over as that type.
func makeEnumTranslator(typeParam string, bind enumComparatorBinding, nullable bool) (APIComparatorTranslator, bool) {
	if bind.GoTypeName == "" || bind.GoTypeName != typeParam {
		return APIComparatorTranslator{}, false
	}
	inputType := enumComparatorInput(bind.GraphQLName)
	goType := bind.QualifiedGoType
	tr := APIComparatorTranslator{
		FuncName:      "translate" + inputType,
		InputTypeName: inputType,
		Family:        "Enum",
		Nullable:      false,
		EnumT:         goType,
		GoReturnType:  "*comparator.Enum[" + goType + "]",
		OutStructName: "comparator.Enum[" + goType + "]",
	}
	if !nullable {
		return tr, true
	}
	tr.FuncName = "translateNullable" + inputType
	tr.InputTypeName = nullableComparatorInput(inputType)
	tr.Nullable = true
	tr.GoReturnType = "*comparator.NullableEnum[" + goType + "]"
	tr.OutStructName = "comparator.NullableEnum[" + goType + "]"
	return tr, true
}

// enumComparatorBinding pairs a column's schema enum with the two names the
// enum comparator projection needs: the GraphQL enum the operands are typed
// with, and the models-qualified Go type the translator's type parameter
// spells. Resolved by mapColumnToGraphQL from typeBindings, because neither
// name is recoverable from the `*comparator.Enum[T]` expression alone.
//
// The zero value means "this column's Go type is not a schema enum".
type enumComparatorBinding struct {
	// GraphQLName is the GraphQL enum type name — identical to the bare Go
	// enum type name (buildAPIEnumContext) and the base of the monomorphized
	// input type name.
	GraphQLName string
	// GoTypeName is the bare Go enum type name, as resolveComparatorType
	// spells the type parameter.
	GoTypeName string
	// QualifiedGoType is GoTypeName under the consumer's models package
	// alias, as the generated graph package must spell it.
	QualifiedGoType string
}

// enumComparatorBindingFor projects a resolved schema enum onto the binding
// the comparator projection consumes. Returns the zero value when the column
// resolved to no enum, which is what makes makeEnumTranslator decline — and so
// what drops an enum-LIKE Go type that is not a schema enum off the GraphQL
// filter surface.
func enumComparatorBindingFor(enum EnumContext, ok bool, modelsPkg string) enumComparatorBinding {
	if !ok {
		return enumComparatorBinding{}
	}
	return enumComparatorBinding{
		GraphQLName:     enum.GoTypeName,
		GoTypeName:      enum.GoTypeName,
		QualifiedGoType: modelsPkg + "." + enum.GoTypeName,
	}
}

// enumComparatorInput names the monomorphized comparator input for one enum
// (PRD §26.4 Rule 1 — `<EnumName>Comparator`), disambiguating to
// `<EnumName>EnumComparator` when the derived name is one of the fixed
// families'.
//
// Without the guard an enum PascalCasing to `Duration`, `Time`, `Numeric`,
// `Decimal`, `IP` or `CIDR` silently inherits that family's operand set:
// comparatorOperatorsFor short-circuits on comparatorFamilyByName, so the
// schema advertises `gt` / `lt` / `between` on a column whose Enum translator
// copies only eq/neq/in/nin, and translatorsByName is last-write-wins between
// the two same-named translators. Nothing errors — the schema is valid, the Go
// compiles, and the extra operators are accepted and dropped.
//
// `<Elem>SliceComparator` needs no equivalent: that name always carries `Slice`
// before `Comparator`, so it cannot equal any fixed family name whatever the
// element type is. Enum monomorphization is the only row that can reach the
// fixed set.
func enumComparatorInput(graphQLName string) string {
	if name := graphQLName + "Comparator"; !fixedOwnedGraphQLName(name) {
		return name
	}
	return graphQLName + "EnumComparator"
}

// enumComparatorFamily derives the `<Enum>Comparator` family declaration for
// one schema enum. Operands are typed with the bound GraphQL enum rather than
// with String, which is the whole point of monomorphizing: `{status: {eq:
// BANANA}}` is rejected by the GraphQL parser instead of reaching a resolver.
//
// The operator set is comparator.Enum[T]'s, minus the `Custom` raw-SQL escape
// hatch every family carries. There is no static entry in
// shippedComparatorFamilies because the set is per-enum and therefore
// per-project; collectComparatorFamilies picks it up off the referencing
// column's projection instead.
func enumComparatorFamily(graphQLName string) APIComparatorFamily {
	return APIComparatorFamily{
		Name: enumComparatorInput(graphQLName),
		Operators: []APIComparatorOperator{
			{Name: "eq", Type: graphQLName},
			{Name: "neq", Type: graphQLName},
			{Name: "in", Type: "[" + graphQLName + "!]"},
			{Name: "nin", Type: "[" + graphQLName + "!]"},
		},
	}
}

// sliceComparatorInput names the monomorphized comparator input for an array
// column whose ELEMENT projects onto elemGraphQL (PRD §26.4 Rule 1 —
// `<Elem>SliceComparator`).
//
// This needs no equivalent of enumComparatorInput's collision guard, and PRD
// §26.4 says so explicitly: the name always carries `Slice` immediately before
// `Comparator`, so it cannot equal any fixed family name whatever the element
// type is. Enum monomorphization is the only row that can reach the fixed set.
func sliceComparatorInput(elemGraphQL string) string {
	return elemGraphQL + "SliceComparator"
}

// sliceComparatorFamily derives the `<Elem>SliceComparator` family declaration
// for one array element type. Like the enum families it has no static entry in
// shippedComparatorFamilies — the operand type is the element's own GraphQL
// type and is therefore per-project — so collectComparatorFamilies picks it up
// off the referencing column's projection instead.
//
// `isEmpty` is a Boolean PREDICATE, not an element operand: it projects
// comparator.Slice's `IsEmpty *bool`, which compiles to `col = '{}'` /
// `col != '{}'` rather than to an array comparison. It is the one operator in
// the set whose type does not move with the element.
func sliceComparatorFamily(elemGraphQL string) APIComparatorFamily {
	list := "[" + elemGraphQL + "!]"
	return APIComparatorFamily{
		Name: sliceComparatorInput(elemGraphQL),
		Operators: []APIComparatorOperator{
			{Name: "containsAny", Type: list},
			{Name: "containsAll", Type: list},
			{Name: "containedBy", Type: list},
			{Name: "isEmpty", Type: "Boolean"},
		},
	}
}

// sliceOperandElemGoType maps an array element's GraphQL type to the Go type
// gqlgen binds a NON-NULL LIST ELEMENT of it to, and reports false for a type
// with no such binding.
//
// Only the GraphQL spec built-ins are listed, and that is the whole gate. A
// custom scalar element (`Time`, `UUID`, `Decimal`, `Bytes`, `IP`) follows
// gqlgen's other list rule — a STRUCT element is pointer-wrapped inside a list
// while a nilable or basic-kind one is not (see opaqueComparatorBindings for
// the measurement and the counterexample) — and sqlgen has not measured those
// shapes for the Slice family. Returning false drops such a column off the
// GraphQL filter surface rather than emitting an operand block that does not
// compile, exactly as makeOpaqueTranslator does for an unknown `T`.
//
// A schema-enum element does NOT come through here: it is resolved from the
// column's enum binding before this is consulted, because gqlgen binds a
// non-null enum list element bare (`[]models.X`, measured) and the
// element type is the consumer's, not one this table could name.
func sliceOperandElemGoType(elemGraphQL string) (string, bool) {
	switch elemGraphQL {
	case "String", "ID":
		return "string", true
	case "Boolean":
		return "bool", true
	case "Int":
		return "int", true
	case "Float":
		return "float64", true
	}
	return "", false
}

// makeSliceTranslator builds the translator descriptor for one
// comparator.Slice[T] monomorphization (PRD §26.4 Rule 1, §26.5.3). The second
// return is false when the element's operand shape is not one sqlgen has
// measured, which keeps the column non-projected on both sides rather than
// advertising operands the generated body cannot copy.
//
// The INPUT is keyed on the element's GraphQL type and the TRANSLATOR on its
// Go type, which is the same split NumericComparator already carries and for
// the same reason: `smallint[]`, `integer[]` and `bigint[]` all bind their
// elements to `Int`, so one `IntSliceComparator` serves all three (PRD §26.4
// "Sharing" keys on the operand type) while each needs its own
// `comparator.Slice[T]`. The Go suffix is therefore appended exactly when a
// width conversion is needed — which is exactly when the input is shared —
// giving `translateIntSliceComparatorInt32` beside a plain
// `translateStringSliceComparator`.
func makeSliceTranslator(typeParam string, bind enumComparatorBinding, elemGraphQL string, nullable bool) (APIComparatorTranslator, bool) {
	inputType := sliceComparatorInput(elemGraphQL)
	suffix := ""
	elemT := typeParam
	elemIsEnum := bind.GoTypeName != "" && bind.GoTypeName == typeParam
	if elemIsEnum {
		// The type parameter is the consumer's own enum type, so the graph
		// package has to spell it under the models alias — the same reason
		// APIComparatorTranslator.EnumT is qualified where NumericT is not.
		elemT = bind.QualifiedGoType
	} else {
		gqlgenElem, ok := sliceOperandElemGoType(elemGraphQL)
		if !ok {
			return APIComparatorTranslator{}, false
		}
		if gqlgenElem != typeParam {
			// The only licensed disagreement is a numeric width, and it is the
			// same table the create/update input translator consults — so a
			// pair this does not license cannot be bridged there either.
			if !isNumericWidthCast(gqlgenElem, typeParam) {
				return APIComparatorTranslator{}, false
			}
			suffix = numericSuffixForType(typeParam)
		}
	}

	tr := APIComparatorTranslator{
		FuncName:         "translate" + inputType + suffix,
		InputTypeName:    inputType,
		Family:           "Slice",
		Nullable:         false,
		SliceT:           elemT,
		SliceElemGraphQL: elemGraphQL,
		SliceElemCast:    suffix != "",
		SliceElemIsEnum:  elemIsEnum,
		GoReturnType:     "*comparator.Slice[" + elemT + "]",
		OutStructName:    "comparator.Slice[" + elemT + "]",
	}
	if !nullable {
		return tr, true
	}
	tr.FuncName = "translateNullable" + inputType + suffix
	tr.InputTypeName = nullableComparatorInput(inputType)
	tr.Nullable = true
	tr.GoReturnType = "*comparator.NullableSlice[" + elemT + "]"
	tr.OutStructName = "comparator.NullableSlice[" + elemT + "]"
	return tr, true
}

// makeDurationTranslator builds the translator descriptor for a duration
// column. It is a Number translator in everything but naming: the model side
// really is comparator.Number[time.Duration], and only the GraphQL input type
// and func name differ, which is what keeps a duration column's filter field
// off NumericComparator's `Float` operands.
func makeDurationTranslator(nullable bool) APIComparatorTranslator {
	tr := APIComparatorTranslator{
		FuncName:      "translateDurationComparator",
		InputTypeName: "DurationComparator",
		Family:        "Number",
		Nullable:      false,
		NumericT:      durationGoType,
		GoReturnType:  "*comparator.Number[" + durationGoType + "]",
		OutStructName: "comparator.Number[" + durationGoType + "]",
	}
	if !nullable {
		return tr
	}
	tr.FuncName = "translateNullableDurationComparator"
	tr.InputTypeName = nullableComparatorInput("DurationComparator")
	tr.Nullable = true
	tr.GoReturnType = "*comparator.NullableNumber[" + durationGoType + "]"
	tr.OutStructName = "comparator.NullableNumber[" + durationGoType + "]"
	return tr
}

// makeNonNumericTranslator builds the translator descriptor for the four
// non-generic comparator families. `family` is the comparator package's
// struct base name (e.g. "String"); `inputType` is the GraphQL/gqlgen input
// type name (e.g. "StringComparator").
func makeNonNumericTranslator(family, inputType string, nullable bool) APIComparatorTranslator {
	base := APIComparatorTranslator{
		FuncName:      "translate" + inputType,
		InputTypeName: inputType,
		Family:        family,
		Nullable:      false,
		GoReturnType:  "*comparator." + family,
		OutStructName: "comparator." + family,
	}
	if !nullable {
		return base
	}
	wrapper := APIComparatorTranslator{
		FuncName:      "translateNullable" + inputType,
		InputTypeName: nullableComparatorInput(inputType),
		Family:        family,
		Nullable:      true,
		GoReturnType:  "*comparator.Nullable" + family,
		OutStructName: "comparator.Nullable" + family,
	}
	return wrapper
}

// makeDecimalTranslator builds the translator descriptor for a decimal
// column (PRD §26.4 "Decimal columns", §26.5.3). `decimal.Decimal` does not
// satisfy `comparator.Numeric`, so the model side filters on the canonical
// string form through `comparator.String` — but the GraphQL operands stay
// `Decimal`-typed, which is what makes `{ gt: "100.50" }` compare
// numerically: the database coerces the parameter against a numeric column
// rather than collating it as text.
//
// The return types are String / NullableString, identical to the plain
// string families; only the input type and the func name differ, which is
// what keeps a decimal column's filter field off StringComparator and its
// meaningless text operators.
func makeDecimalTranslator(nullable bool) APIComparatorTranslator {
	base := APIComparatorTranslator{
		FuncName:      "translateDecimalComparator",
		InputTypeName: "DecimalComparator",
		Family:        "Decimal",
		Nullable:      false,
		GoReturnType:  "*comparator.String",
		OutStructName: "comparator.String",
	}
	if !nullable {
		return base
	}
	return APIComparatorTranslator{
		FuncName:      "translateNullableDecimalComparator",
		InputTypeName: nullableComparatorInput("DecimalComparator"),
		Family:        "Decimal",
		Nullable:      true,
		GoReturnType:  "*comparator.NullableString",
		OutStructName: "comparator.NullableString",
	}
}

// makeNumericTranslator builds the translator descriptor for one Numeric
// comparator variant (per Go T parameter). Function-name suffix is the
// PascalCase form of the T parameter so distinct numeric types produce
// distinct translator functions in the same file.
func makeNumericTranslator(typeParam string, nullable bool) APIComparatorTranslator {
	suffix := numericSuffixForType(typeParam)
	base := APIComparatorTranslator{
		FuncName:      "translateNumericComparator" + suffix,
		InputTypeName: "NumericComparator",
		Family:        "Number",
		Nullable:      false,
		NumericT:      typeParam,
		GoReturnType:  "*comparator.Number[" + typeParam + "]",
		OutStructName: "comparator.Number[" + typeParam + "]",
	}
	if !nullable {
		return base
	}
	return APIComparatorTranslator{
		FuncName:      "translateNullableNumericComparator" + suffix,
		InputTypeName: nullableComparatorInput("NumericComparator"),
		Family:        "Number",
		Nullable:      true,
		NumericT:      typeParam,
		GoReturnType:  "*comparator.NullableNumber[" + typeParam + "]",
		OutStructName: "comparator.NullableNumber[" + typeParam + "]",
	}
}

// numericSuffixForType produces a Go-identifier-safe PascalCase suffix for
// a numeric type parameter (e.g. "int32" → "Int32", "float64" → "Float64").
func numericSuffixForType(t string) string {
	switch t {
	case "int":
		return "Int"
	case "int8":
		return "Int8"
	case "int16":
		return "Int16"
	case "int32":
		return "Int32"
	case "int64":
		return "Int64"
	case "uint":
		return "Uint"
	case "uint8":
		return "Uint8"
	case "uint16":
		return "Uint16"
	case "uint32":
		return "Uint32"
	case "uint64":
		return "Uint64"
	case "float32":
		return "Float32"
	case "float64":
		return "Float64"
	}
	return toPascalCase(t)
}

// buildUpdateOps assembles the per-numeric-column dispatch metadata used by
// the resolver template to emit conflict checks and Increment calls. Numeric
// non-PK columns only — every entry here corresponds to one row of the
// generated `<Table>IncrementColumn` enum.
//
// The `_inc` / `_dec` Go field names are a literal "Inc" / "Dec" suffix on the
// cased column name. That is safe because sqlgen does not have to predict what
// gqlgen would derive from `<field>_inc` — it dictates the name through the
// fieldName override keyed by IncGraphQLName / DecGraphQLName
// (APIGoFieldOverrides, PRD §26.5.6). Before that mechanism, an acronym
// matching either suffix silently desynchronized the two sides: flect's DEC
// made gqlgen emit "StockDEC" against this template's "StockDec".
// The tenant column needs no skip here: an integer tenant column is
// incrementable and Increment constrains which rows it matches but not which
// column it targets, so `tenantID_inc: 1` would walk a row out of its own
// tenant — which is why isIncrementEligible drops it on the model side, ahead
// of apiIncrementColumns (PRD §29.4.2). Re-testing it here would be
// the second derivation that let the two surfaces disagree in the first place.
func buildUpdateOps(tc TableContext, gqlNameByColumn map[string]string) []APIUpdateOp {
	columns := apiIncrementColumns(tc)
	ops := make([]APIUpdateOp, 0, len(columns))
	for _, col := range columns {
		// §32.2: `_inc` / `_dec` operators are emitted only for API-writable
		// numeric columns — the schema-side gate (`$f.InUpdateInput` plus
		// `$f.Numeric`) and this resolver/seed-side list must stay in sync.
		if !columnAccessCapabilities(col).APIWritable {
			continue
		}
		base := col.FieldName
		gqlName := gqlNameByColumn[col.Name]
		ops = append(ops, APIUpdateOp{
			SQLName:        col.Name,
			IncGraphQLName: gqlName + "_inc",
			DecGraphQLName: gqlName + "_dec",
			SetGoField:     base,
			IncGoField:     base + "Inc",
			DecGoField:     base + "Dec",
			IncColumnConst: tc.StructName + "Increment" + col.FieldName,
		})
	}
	return ops
}

// inputDroppedColumns returns the column names to omit from an API input:
// the PK columns unless caller PKs are kept (a caller-strategy PK is
// client-supplied, so every PK column on such a table stays in the create
// input), plus the server-owned tenant column when one is named.
func inputDroppedColumns(columns []ColumnContext, keepCallerPKs bool, dropTenantColumn string) map[string]bool {
	out := make(map[string]bool, len(columns))
	if !keepCallerPKs {
		for _, c := range columns {
			if c.PrimaryKey {
				out[c.Name] = true
			}
		}
	}
	if dropTenantColumn != "" {
		out[dropTenantColumn] = true
	}
	return out
}

// buildAPIInputFields converts a slice of InputFieldContext entries (which
// describe the consumer model's CreateInput / UpdateInput shape) into the
// gqlgen-side translation metadata the resolver template walks.
//
// Server-generated PK columns are filtered out: the GraphQL schema
// CreateInput excludes them per `gen/templates/api/schema.graphqls.tmpl`
// (a DB- or app-strategy PK is API-layer-generated, never client-supplied),
// so the gqlgen-emitted Go input has no field for them. The model side may
// still hold a PK `omittable.Value[…]` field (when DB-strategy populates it
// from a default like `gen_random_uuid()`); the translator simply leaves
// that field at its zero value, which the model's downstream code
// understands.
//
// `keepCallerPKs` inverts that for a caller-strategy PK (always the case for
// a composite FK tuple): the client supplies those values, gqlgen emits Go
// fields for them, and the translator must copy them through — otherwise the
// junction row is written with zero-valued keys. Passed true only for the
// create input; the update input never carries PKs.
//
// `dropTenantColumn`, when non-empty, additionally removes the server-owned
// tenant column (PRD §29.4.2). Passed only for the update input: gqlgen emits
// no field for it there, so `translateUpdate<T>Input` and
// `hasUpdate<T>SetFields` must stop referencing one.
//
// `columns` provides the original `ColumnContext` slice so the function can
// look up `col.Nullable` (schema-side nullability) per input field — neither
// `InputFieldContext` nor `APIInputField` track this on their own.
//
// `scalarByColumn` carries the GraphQL type each column's FIELD resolved to,
// keyed by SQL column name, threaded down from the same mapColumnToGraphQL
// pass that produced the schema. The coercion has to be decided against the
// binding the schema actually advertises; re-deriving it here would let the
// two diverge one type at a time.
func buildAPIInputFields(fields []InputFieldContext, columns []ColumnContext, gqlNameByColumn, scalarByColumn map[string]string, modelsPkg string, keepCallerPKs bool, dropTenantColumn string) ([]APIInputField, error) {
	colByName := make(map[string]ColumnContext, len(columns))
	for _, c := range columns {
		colByName[c.Name] = c
	}
	dropped := inputDroppedColumns(columns, keepCallerPKs, dropTenantColumn)

	out := make([]APIInputField, 0, len(fields))
	for _, f := range fields {
		if dropped[f.ColumnName] {
			continue
		}
		col, ok := colByName[f.ColumnName]
		if !ok {
			continue
		}
		if !columnAccessCapabilities(col).APIWritable {
			// §32.2: the column's access role drops it from the API
			// create/update inputs. The schema-side `$f.Writable` gate
			// mirrors this skip, so the gqlgen input type and the translator
			// stay in sync. The model input keeps the field — the server-side
			// Go client is never narrowed.
			continue
		}

		// `InputFieldContext.GoType` is `omittable.Value[T]` for omittable
		// fields and bare `T` for required ones. Strip the wrapper so we can
		// inspect the inner type (pointer vs value).
		innerType := f.GoType
		if inner, hasWrap := strings.CutPrefix(innerType, "omittable.Value["); hasWrap {
			innerType = strings.TrimSuffix(inner, "]")
		}
		innerIsPointer := strings.HasPrefix(innerType, "*")
		bareGoType := strings.TrimPrefix(innerType, "*")

		coerce, err := inputCoercionFor(col, scalarByColumn[f.ColumnName], bareGoType, innerIsPointer, modelsPkg)
		if err != nil {
			return nil, err
		}

		out = append(out, APIInputField{
			GraphQLName:          gqlNameByColumn[f.ColumnName],
			GoFieldName:          f.FieldName,
			ModelFieldName:       f.FieldName,
			GoType:               bareGoType,
			SchemaNullable:       col.Nullable,
			ModelOmittable:       f.Omittable,
			ModelInnerIsPointer:  innerIsPointer,
			CastTo:               coerce.CastTo,
			AddressOf:            coerce.AddressOf,
			PointerCast:          coerce.PointerCast,
			GqlgenIsBareNullable: coerce.GqlgenIsBareNullable,
			GqlgenGoType:         coerce.GqlgenGoType,
		})
	}
	return out, nil
}

// inputCoercion carries the four flags that decide how one create/update
// input field is coerced from its gqlgen-emitted shape onto the model's.
// Extracted from buildAPIInputFields so the derivation reads as one unit and
// the caller stays under the complexity ceiling.
type inputCoercion struct {
	CastTo               string
	AddressOf            bool
	PointerCast          bool
	GqlgenIsBareNullable bool
	GqlgenGoType         string
}

// inputCoercionFor derives the coercion flags for a single input field by
// COMPARING the Go type gqlgen emits against the Go type the model wants —
// never by pattern-matching the model type alone.
//
// Pattern-matching the model type alone misses every width or binding the
// list does not name. `scalar` is the GraphQL type the very same column
// resolved to in mapColumnToGraphQL, threaded down rather than
// recomputed, so the schema this input is validated against and the coercion
// that fills it are derived from one decision.
//
// Exactly three outcomes are possible, and the default one is an error:
//
//   - The two types agree. Nothing to do: a custom scalar (registry or
//     consumer-declared) and a schema enum both bind gqlgen to the model's own
//     Go type through the wrapper-merged `models:` entry, so no coercion
//     exists to get wrong.
//   - They disagree in a declared way — a numeric width (gqlgenNumericWidthCasts)
//     or the PostgreSQL enum-array named-slice bridge. A plain Go conversion
//     is the intended bridge; emit it.
//   - They disagree in any other way. There is no sound expression the
//     translator could emit, so this returns an error instead of emitting one
//     that does not compile (`[]byte`, `time.Duration`, `net.IPNet`, the
//     `sql.NullX` family) or one that compiles and produces garbage
//     (`net.IP(someString)` reinterprets the text's raw bytes). Registering
//     the Go type in builtInScalarRegistry or declaring it under
//     `api.graphql.scalars` is what makes the first outcome apply.
func inputCoercionFor(col ColumnContext, scalar, bareGoType string, innerIsPointer bool, modelsPkg string) (inputCoercion, error) {
	var c inputCoercion

	c.GqlgenGoType = dictatedInputGoType(col, scalar, bareGoType)
	gqlgenGoType := gqlgenInputGoType(col, scalar, bareGoType)
	switch {
	case gqlgenGoType == bareGoType:
		// Identity. Covers the spec built-ins gqlgen binds directly (`string`,
		// `bool`, `int`, `float64`), every registry and consumer scalar, every
		// schema enum, a non-enum array column whose model type is the same
		// `[]T` gqlgen emits, and every numeric array, whose
		// generated field type sqlgen dictates to the model's own.

	case setElemGoType(col) != "",
		col.IsSlice && strings.HasSuffix(bareGoType, "Slice") && col.SliceElemType != "":
		// Named-slice columns. gqlgen emits `[]<ElemGoType>` for a
		// `[<EnumGraphQL>!]!` field, but the model uses a named slice — a
		// PostgreSQL enum array's `<EnumGoType>Slice` or a MySQL SET's
		// `<Table><Column>Set` — because that is where `driver.Valuer` /
		// `sql.Scanner` live. The named slice's underlying type IS
		// `[]<ElemGoType>`, so the conversion is a zero-cost reinterpret.
		c.CastTo = modelsPkg + "." + bareGoType

	case isNumericWidthCast(gqlgenGoType, bareGoType):
		// The column's native width differs from the one gqlgen binds to the
		// numeric scalar (integer widths, `float32`). Both are consequences of
		// this comparison rather than entries in a list.
		//
		// Note this is narrower than the predicate it replaces: a column
		// routed onto a consumer-declared scalar binds gqlgen to the model's
		// own Go type, so it takes the identity arm instead of emitting a
		// no-op self-conversion `unconvert` would flag.
		//
		// That holds for EVERY consumer scalar, including the ones gqlgen
		// bundles. It did not always: gqlgen's `Int64` is an extraBuiltin
		// whose model list is `[graphql.Int, graphql.Int64]`, and a generated
		// position takes `Model[0]`, so a column routed onto it came out `int`
		// against an `int64` model and the identity arm emitted a
		// non-compiling `out.X = in.X`. scalarModelPath now pins the bundled
		// marshaler, which collapses that list to the declared Go type — the
		// premise gqlgenGoTypeFor states is true for these too.
		c.CastTo = bareGoType

	case col.IsSlice:
		// An array column whose element types disagree and which
		// dictatedInputGoType declined to dictate — i.e. the element is not a
		// spec numeric, so `models.<Input>.fields.<f>.type` has no sound value
		// to carry. Go has no conversion between slices of differing element
		// types, so this cannot be a cast, and it is a distinct diagnosis from
		// the scalar case below. Naming the ELEMENT is what makes the message
		// actionable: `api.graphql.scalars` is keyed on the element Go type, so
		// telling the consumer to bind `[]T` would send them to a lookup that
		// never reads it.
		//
		// The numeric arrays (`smallint[]` / `integer[]` /
		// `bigint[]` / `real[]`) do not reach here — they take the identity
		// arm above.
		return inputCoercion{}, fmt.Errorf(
			"api: array column %q has element Go type %q, but its GraphQL element type %q binds "+
				"gqlgen to %q; Go cannot convert between slices of differing element types, so the "+
				"input translator has no sound form. Declare the ELEMENT type under "+
				"`api.graphql.scalars`, or retype the column via `overrides.types` (PRD §26.4.1)",
			col.Name, strings.TrimPrefix(bareGoType, "[]"), scalar, strings.TrimPrefix(gqlgenGoType, "[]"),
		)

	default:
		return inputCoercion{}, fmt.Errorf(
			"api: column %q resolves to Go type %q but its GraphQL type %q binds gqlgen to %q, "+
				"and no conversion between them is defined; register the Go type in the built-in "+
				"scalar registry, or declare it under `api.graphql.scalars` so gqlgen binds the "+
				"scalar to the model's own type (PRD §26.4.1)",
			col.Name, bareGoType, scalar, gqlgenGoType,
		)
	}

	// gqlgen leaves a nullable field's Go type unwrapped when the type already
	// carries a nil state, because the nil-slice / nil-map / nil-pointer
	// already encodes "null" (v0.17.90 `binder.go`, IsNilable +
	// CopyModifiersFromAst). This holds regardless of the column's SQL
	// nullability: a `NOT NULL DEFAULT` JSONB column is optional in the input
	// by virtue of its default (so the schema field is nullable) yet
	// col.Nullable is false — the deref guard must not hinge on col.Nullable.
	//
	// Keyed on the GQLGEN type, not the model type: for a list column the two
	// differ (`[]Enum` vs `EnumSlice`) and it is gqlgen's shape the translator
	// dereferences or does not.
	c.GqlgenIsBareNullable = isNilableGqlgenGoType(gqlgenGoType)

	// gqlgen emits bare `T` but the model wraps a pointer
	// (`omittable.Value[*json.RawMessage]`) to keep "not provided" distinct
	// from "explicit null". Bridge by taking the address inside
	// `omittable.Set(...)`.
	c.AddressOf = c.GqlgenIsBareNullable && innerIsPointer

	// A pointer-to-pointer hand-off is only safe when the pointee
	// types match. When the model narrows the width (`*int32`, `*float32`) and
	// gqlgen does not (`*int`, `*float64`), the cast has to happen through a
	// local — Go cannot convert a pointee in expression position. The
	// bare-nullable and address-of branches apply their own cast, so they are
	// excluded here.
	c.PointerCast = innerIsPointer && c.CastTo != "" && !c.GqlgenIsBareNullable

	return c, nil
}

// resolveColumnBinding reduces a column's model Go type to the form the scalar
// lookup keys on, and resolves its GraphQL binding. Returns the bare Go type,
// the GraphQL type, and the scalar use (nil for a spec built-in or an enum).
//
// The `[]` strip applies to ARRAY columns only. For those the element binds
// the scalar and the `[...]` wrapper is re-applied by the caller, so stripping
// is what makes `[]DocumentEntityTypeEnum` resolve through the enum lookup.
// `[]byte` is not an array column — nothing re-wraps it — so stripping there
// reduced `bytea` to a lookup for `byte`, which matches nothing. That is why
// the `[]byte` case in graphQLTypeForGoType was dead code and every `bytea` /
// `blob` column took the unknown-type path.
//
// `apiVisible` false means §32.2 dropped the column from every API surface, so
// it names no GraphQL type anywhere in the emitted schema and cannot be the
// column a consumer needs to bind. Demanding a binding for it would make
// `access: hidden` — the documented way to keep a column OUT of the API — the
// one setting that forces you to describe it TO the API. The unused value
// keeps its fallback spelling so no gated-off reader changes behaviour.
func resolveColumnBinding(col ColumnContext, binds typeBindings, apiVisible bool) (bareGoType, graphQLType string, scalar *APIScalarUse, err error) {
	bareGoType = strings.TrimPrefix(col.GoType, "*")
	if col.IsSlice {
		bareGoType = stripPointerAndSlice(col.GoType)
	}
	graphQLType, scalar, err = graphQLTypeForGoType(bareGoType, col, binds)
	if err != nil {
		if apiVisible {
			return "", "", nil, err
		}
		return bareGoType, "String", nil, nil
	}
	return bareGoType, graphQLType, scalar, nil
}

// mapColumnToGraphQL produces an APIFieldContext for one column. Side-effect:
// when the column resolves to a non-spec scalar, scalarsByName / usedInTable
// are updated; when the column resolves to a schema enum, usedEnumsByName is
// updated so APIContext.UsedEnums covers every enum referenced by any exposed
// column.
func mapColumnToGraphQL(col ColumnContext, casing string, dialect config.Dialect, scalarsByName map[string]APIScalarUse, usedInTable map[string]bool, binds typeBindings, usedEnumsByName map[string]EnumContext) (APIFieldContext, string, error) {
	caps := columnAccessCapabilities(col)

	// A column dropped from every API surface (§32.2 hidden/internal)
	// references no scalar or enum through the schema, so skip the registry
	// side-effects — otherwise a secret-only custom scalar would emit an
	// unused declaration + marshaler.
	apiVisible := caps.APIReadable || caps.APIWritable || caps.APIFilterable || caps.APISortable

	bareGoType, bare, scalar, err := resolveColumnBinding(col, binds, apiVisible)
	if err != nil {
		return APIFieldContext{}, "", err
	}
	// The scalar name is returned alongside the field so the create/update
	// input coercion is decided against the binding this schema field
	// advertises, rather than re-derived from the model Go type
	// (inputCoercionFor). Captured before the list wrap: gqlgen binds the
	// scalar to a list's ELEMENT.
	elemScalar := bare
	// Three things drop a column's filter field before the projection is even
	// attempted: `json[]` / `jsonb[]` (cannot satisfy `T comparable`), a JSON
	// column on SQLite (whose comparator family has no arm for that dialect),
	// and §32.2 (an access role that drops the filter surface).
	// The first two come from columnIsFilterable, which the model side reads
	// too, so the two surfaces agree by construction. A fourth drop is decided
	// inside the projection itself — a comparator with no sound GraphQL
	// face — and that is why APIFieldContext.Filterable is read back off the
	// resolved projection below rather than set from this flag.
	filterable := columnIsFilterable(col, dialect) && caps.APIFilterable
	// Resolved before the projection: an enum column's comparator is
	// monomorphized against the bound GraphQL enum (PRD §26.4 Rule 1), and
	// neither that name nor the models-qualified Go type is recoverable from
	// the `*comparator.Enum[T]` expression alone.
	//
	// Covers both the bare-enum and array-of-enum paths because
	// `stripPointerAndSlice` reduces `[]DocumentEntityTypeEnum` to
	// `DocumentEntityTypeEnum` before the lookup. An array column resolves to
	// comparator.Slice, so the binding it carries is simply unused — it is
	// consumed only inside the Enum arm.
	enum, hasEnum := binds.enums[bareGoType]
	// Resolved before the scalar registration below, which needs to know
	// whether this column's comparator actually takes Decimal operands.
	//
	// `bare` is the ELEMENT's GraphQL type at this point — the `[...]` wrap
	// below has not been applied yet — which is exactly what an array column's
	// `<Elem>SliceComparator` is keyed on. Resolving the projection before the
	// wrap is what lets one value serve both the field type and the operand
	// type without a second derivation.
	filter := resolveAPIFilterProjection(col, dialect, scalar, enumComparatorBindingFor(enum, hasEnum, binds.modelsPkg), bare, filterable)
	if scalar != nil && apiVisible {
		registerColumnScalars(col, *scalar, filter, scalarsByName, usedInTable)
	}
	// Record enum usage so the shared-schema template emits the `enum`
	// declaration and the gqlgen models merge binds the GraphQL enum to the
	// consumer's Go enum type.
	if hasEnum && apiVisible {
		usedEnumsByName[enum.GoTypeName] = enum
	}

	if columnIsListShaped(col) {
		// A list column surfaces as `[T!]!` — the list itself is non-null
		// (defaults to empty), elements are non-null. `bare` is already the
		// ELEMENT's GraphQL type at this point, for both list shapes.
		bare = "[" + bare + "!]"
	}
	full := bare
	// Null-variant scalars (NullUUID, NullDecimal, NullDateTime — PRD §26.4.1)
	// carry nullability inside the wrapper struct, so the GraphQL field is
	// always emitted as nullable regardless of col.Nullable.
	if !col.Nullable && (scalar == nil || !scalar.NullVariant) {
		full = bare + "!"
	}

	// PRD §8.5: sortability gates the enum value, not the spelling — an
	// unsortable column carries no enum value at all rather than a value
	// nothing matches.
	sortEnumValue := ""
	if caps.APISortable {
		sortEnumValue = screamingSnakeCase(col.Name)
	}

	// gqlgen binds `Int` to `int` / `int32` / `int64` and `Float` to
	// `float64` only. A column resolving to any other width needs a marshaler
	// anchor or gqlgen answers the field with a panic resolver. Keyed on the
	// ELEMENT scalar and the element Go type, so an array and its scalar
	// sibling ask for the same anchor. Gated on apiVisible for the same reason
	// registerColumnScalars is: a §32.2-hidden column names nothing in the
	// schema, so emitting a marshaler for it would be dead code.
	anchor := numericWidthAnchor(elemScalar, bareGoType, apiVisible)

	return APIFieldContext{
		SQLName:            col.Name,
		GraphQLName:        graphQLFieldName(col.Name, casing),
		GraphQLType:        full,
		GraphQLBare:        bare,
		GoFieldName:        col.FieldName,
		GoFieldOverridden:  col.FieldNameOverridden,
		Nullable:           col.Nullable,
		PrimaryKey:         col.PrimaryKey,
		Filterable:         filter.InputTypeName != "",
		Readable:           caps.APIReadable,
		Writable:           caps.APIWritable,
		Sortable:           caps.APISortable,
		Description:        col.Description,
		Filter:             filter,
		SortEnumValue:      sortEnumValue,
		NumericWidthAnchor: anchor,
	}, elemScalar, nil
}

// resolveAPIFilterProjection resolves a column's complete GraphQL filter
// projection. This is the single point at which a column's comparator input
// type and its translator function are decided (PRD §26.5.3); every
// other site reads the result off APIFieldContext.Filter.
//
// The derivation runs off the model side: resolveComparatorType is the same
// function that produces the column's `*comparator.X[Y]` field on the
// generated model filter (PRD §11.2), so the input type the schema
// advertises is chosen by the very expression the translator has to compile
// against. That ordering is what makes the two surfaces agree by
// construction rather than by convention.
func resolveAPIFilterProjection(col ColumnContext, dialect config.Dialect, scalar *APIScalarUse, enumBind enumComparatorBinding, elemGraphQL string, filterable bool) APIFilterProjection {
	if !filterable {
		return APIFilterProjection{}
	}

	variant, ok := comparatorTranslatorVariant(resolveComparatorType(col, dialect), enumBind, elemGraphQL)
	if !ok {
		// Two shapes reach here: an enum-like Go type that is not a schema
		// enum (makeEnumTranslator's guard), and an array whose element
		// operand shape sqlgen has not measured (sliceOperandElemGoType).
		// Neither has a SOUND projection — there is no input type whose
		// operands the generated translator could copy onto the column's
		// model comparator.
		//
		// The column is therefore dropped from BOTH GraphQL surfaces rather
		// than advertised through a StringComparator with nothing behind it.
		// PRD §26.5.3 makes "absent from both" the only way such a column
		// satisfies the completeness lint — a schema field the translator
		// cannot dispatch is accepted at parse time and ignored at execution
		// time, which is the silent-wrong-results failure the invariant
		// exists to forbid. The model-side `<T>Filter` member is untouched:
		// only the GraphQL face drops, so the Go client keeps filtering the
		// column (PRD §26.12).
		return APIFilterProjection{}
	}
	// PRD §26.4 "Decimal columns": a decimal column's model comparator
	// IS comparator.String (decimal.Decimal does not satisfy
	// comparator.Numeric), but its GraphQL face is
	// DecimalComparator with Decimal-typed operands. Same model type,
	// different input type, because the text operators String carries
	// are meaningless on a numeric column.
	//
	// The narrowing is gated on the resolved family, not on the scalar
	// alone. A decimal-typed column does not always land on String: a
	// decimal FOREIGN KEY resolves to comparator.ID (DeriveFKMethod
	// returns FKStringSprint for decimal.Decimal, so resolveSimpleComparator
	// takes its ID arm), and a `numeric[]` column resolves to
	// comparator.Slice[decimal.Decimal]. Routing either of those to
	// translateDecimalComparator would hand the model filter a
	// *comparator.String where it expects an ID or a Slice, and the
	// generated filter translator would not compile.
	if variant.Family == "String" && isDecimalScalar(scalar) {
		variant = makeDecimalTranslator(col.Nullable)
	}
	proj := APIFilterProjection{
		InputTypeName:  variant.InputTypeName,
		TranslatorFunc: variant.FuncName,
		Translator:     variant,
	}
	proj.Operators = comparatorOperatorsFor(proj, enumBind)
	return proj
}

// comparatorOperatorsFor returns the operator set the projection's input type
// declares. A shipped family is looked up in the family table; the two
// monomorphized families have no static entry — an enum's set is per-enum and
// a slice's per element type, so both are derived from the same descriptor the
// translator was built from.
//
// Every input a projection can name must resolve here: collectComparatorFamilies
// declares the shared-schema block from this list, and an empty one renders
// `input FooComparator {\n}`, which gqlgen rejects.
func comparatorOperatorsFor(proj APIFilterProjection, enumBind enumComparatorBinding) []APIComparatorOperator {
	if fam, ok := comparatorFamilyByName(proj.InputTypeName); ok {
		return fam.Operators
	}
	var fam APIComparatorFamily
	switch proj.Translator.Family {
	case "Enum":
		fam = enumComparatorFamily(enumBind.GraphQLName)
	case "Slice":
		fam = sliceComparatorFamily(proj.Translator.SliceElemGraphQL)
	default:
		return nil
	}
	if proj.Translator.Nullable {
		fam = nullableComparatorTwin(fam)
	}
	return fam.Operators
}

// comparatorOperandScalars lists every GraphQL scalar that can appear as a
// comparator OPERAND type, mapped to its Go binding. Today that is exactly
// two — `TimeComparator` operands are typed `Time` and `DecimalComparator`
// operands are typed `Decimal`; every other family's operands are spec
// built-ins (`String`, `ID`, `Float`, `Boolean`) that need no declaration.
//
// These exist because a comparator's operand type is fixed by its family,
// while a column's own field-type scalar is picked from its Go type — and the
// two disagree. A `types.DateTime` column declares `scalar DateTime` but
// filters through the Time family, whose operands say `Time`; a
// `decimal.NullDecimal` column declares `scalar NullDecimal` but its
// comparator (both forms) takes `Decimal`. Without registering the operand's
// scalar the schema names something it never declares, and gqlgen rejects it.
var comparatorOperandScalars = map[string]struct {
	goType string
	// stdlib marks a binding whose Go type needs no import path — `time.Time`
	// is stdlib, whereas `decimal.Decimal` comes from the column's own
	// package.
	stdlib bool
}{
	"Time":    {goType: "time.Time", stdlib: true},
	"Decimal": {goType: "decimal.Decimal"},
	// The JSON / JSONB families' document operands. Unlike the two above,
	// this one currently cannot disagree with the referencing column's own
	// field scalar: a column reaches those families only by resolving to
	// `types.JSON`, which declares `scalar JSON` for its field anyway. It is
	// listed because the rule is "register whatever scalar each operand type
	// names" — closing over the rule is what stopped the Time gap
	// being rediscovered, and leaving a family out because its two scalars
	// happen to coincide today reopens it.
	"JSON": {goType: "types.JSON"},
}

// registerColumnScalars records every scalar a column's emitted surface
// references: its own field-type scalar, plus the scalar named by each of its
// comparator's operand types (see comparatorOperandScalars).
//
// The second half is not the same as the first. A column's field type and its
// comparator's operands are chosen by different rules — graphQLTypeForGoType
// keys on the Go type, resolveSimpleComparator keys on the column — so a
// schema can reference an operand scalar no column's field type produces.
// Registering both from the resolved projection keeps declaration and
// reference in step (PRD §26.4 Rule 3).
func registerColumnScalars(col ColumnContext, scalar APIScalarUse, filter APIFilterProjection, scalarsByName map[string]APIScalarUse, usedInTable map[string]bool) {
	registerScalarUse(scalar, col.Import, scalarsByName)
	usedInTable[scalar.Name] = true

	if col.PrimaryKey {
		// A PK column is excluded from the filter input on both sides
		// (schema.graphqls.tmpl gates on `not $f.PrimaryKey`, and
		// collectComparatorFamilies does the same), so it references no
		// comparator input and creates no operand-scalar obligation.
		// Registering from its projection would declare a scalar nothing
		// names — the unused-declaration hazard the API-visibility guard
		// above exists to avoid.
		return
	}

	for _, op := range filter.Operators {
		// Operand types are spelled in schema syntax; `[Time!]` names the
		// same scalar as `Time`.
		binding, ok := comparatorOperandScalars[strings.Trim(op.Type, "[]!")]
		if !ok {
			continue
		}
		reg, ok := builtInScalarRegistry[binding.goType]
		if !ok {
			continue
		}
		importPath := col.Import
		if binding.stdlib {
			importPath = ""
		}
		registerScalarUse(APIScalarUse{
			Name:            reg.Name,
			GoType:          binding.goType,
			GoImport:        importPath,
			Marshaling:      reg.Marshaling,
			NullVariant:     reg.NullVariant,
			UnderlyingField: reg.UnderlyingField,
		}, importPath, scalarsByName)
		usedInTable[reg.Name] = true
	}
}

// isDecimalScalar reports whether a column's resolved GraphQL scalar is the
// decimal pair. Keyed on the scalar rather than the Go type so an override
// that binds a column to decimal.Decimal / decimal.NullDecimal routes the
// same way a built-in `numeric` column does — both land on the same registry
// entry, and the registry is what the schema and gqlgen agree on.
func isDecimalScalar(scalar *APIScalarUse) bool {
	if scalar == nil {
		return false
	}
	return scalar.Name == "Decimal" || scalar.Name == "NullDecimal"
}

// serverOwnedTenantColumn returns the name of the tenant column the GraphQL
// update surface must omit, or "" when the table has none to omit.
//
// The three conditions are exactly when a caller-supplied tenant value can
// provably never take effect:
//
//   - Tenanted — an untenanted table has no tenant column at all.
//   - Not in the primary key — a tenant-in-PK table never carried the column
//     on its update input (buildUpdateInputFields drops every PK), and §29.7
//     verify-matches the PK argument instead.
//   - tenancy.required — under `required: true`, resolveTenant returns
//     ErrMissing rather than apply=false for a zero/absent resolver, so the
//     runtime's `if !applyTenancy` escape (which DOES honor a caller-supplied
//     value) is unreachable. CallOptions.SkipTenancy is the other way to
//     reach it and is deliberately not exposed over HTTP. Under
//     `required: false` the escape is live and the field is meaningful, so
//     it stays.
//
// All three are known at codegen, matching the per-table
// required-at-codegen determinism client.go.tmpl already relies on.
func serverOwnedTenantColumn(tc TableContext) string {
	t := tc.Tenancy
	if t == nil || !t.Tenanted || t.InPrimaryKey || !t.Required {
		return ""
	}
	return t.Column
}

// apiFieldInUpdateInput reports whether a column belongs in the GraphQL
// update input. Mirrors buildUpdateInputFields' PK skip on the model side,
// plus the server-owned tenant exclusion above.
func apiFieldInUpdateInput(col ColumnContext, writable bool, tenantCol string) bool {
	return writable && !col.PrimaryKey && col.Name != tenantCol
}

// apiFieldInCreateInput reports whether a column belongs in the GraphQL
// create input. `callerPK` is true when the table's PK strategy is
// `config.PKStrategyCaller`, in which case PK columns are caller-supplied
// (never server-generated) and must be part of the input — otherwise the
// client has no way to name the row it is creating. Mirrors the model-side
// classification in buildCreateInputFields (PRD §26.4).
func apiFieldInCreateInput(col ColumnContext, writable, callerPK bool) bool {
	return writable && (!col.PrimaryKey || callerPK)
}

// graphQLTypeForGoType returns the bare (non-null-stripped) GraphQL type for
// a Go type binding. The second return is non-nil when the type resolves to a
// non-spec scalar that needs registry tracking.
//
// Null-wrapper Go types (uuid.NullUUID / decimal.NullDecimal / types.NullDateTime)
// resolve to their paired Null<Scalar> registry entry — the registry keys
// each wrapper Go type separately, so stripPointerAndSlice (which strips `*`
// and `[]` only) preserves the distinction between `uuid.UUID` and
// `uuid.NullUUID`.
//
// Consumer-declared scalars (`api.graphql.scalars`) have no such pairing: a
// nullable column bound to one emits a nullable field, and gqlgen wraps the
// Go type in a pointer on both the row struct and the generated input — the
// ordinary pointer nullability the Null-wrapper machinery exists to work
// around for struct wrappers.
func graphQLTypeForGoType(goType string, col ColumnContext, binds typeBindings) (string, *APIScalarUse, error) {
	// Schema enums project onto a GraphQL enum named after the Go type
	// (e.g. `DocumentEntityTypeEnum`). gqlgen's models: binding wires the
	// GraphQL enum back to this Go type so the runtime round-trip is direct
	// — no separate resolver stub. Lookup happens before the scalar registry
	// so an enum named identically to a built-in scalar (no current case)
	// resolves consistently with the rest of the codegen.
	if e, ok := binds.enums[goType]; ok {
		return e.GoTypeName, nil, nil
	}
	// Look up the registry first.
	if reg, ok := builtInScalarRegistry[goType]; ok {
		use := APIScalarUse{
			Name:            reg.Name,
			GoType:          goType,
			GoImport:        col.Import,
			Marshaling:      reg.Marshaling,
			NullVariant:     reg.NullVariant,
			UnderlyingField: reg.UnderlyingField,
		}
		return reg.Name, &use, nil
	}
	// Then consumer declarations. After the registry because a `go_type` the
	// registry already owns is a config error (config.BuiltInScalarGoTypes),
	// so the two can never disagree here and the registry stays the single
	// thing the schema and gqlgen agree on. Before the spec built-ins so a
	// declaration can also retype a primitive-bound column onto a scalar of
	// the consumer's own.
	//
	// Routing onto a scalar GQLGEN bundles (`Int64`) goes through the same
	// path: `marshaling: builtin` merges a `models:` entry naming gqlgen's own
	// marshaler for the declared Go type, which is what keeps gqlgen's
	// extraBuiltin list — `[graphql.Int, graphql.Int64]` for `Int64` — from
	// deciding the generated input field's type instead.
	if _, typeName := config.SplitGoType(goType); typeName != "" {
		if cs, ok := binds.scalars[scalarKey{Import: col.Import, TypeName: typeName}]; ok {
			use := cs
			use.GoType = goType
			use.GoImport = col.Import
			return use.Name, &use, nil
		}
	}

	// Spec built-ins.
	switch goType {
	case "string":
		// PK strings carry the `ID` scalar; non-PK strings stay `String`.
		if col.PrimaryKey {
			return "ID", nil, nil
		}
		return "String", nil, nil
	case "bool":
		return "Boolean", nil, nil
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64":
		if col.PrimaryKey {
			return "Int", nil, nil
		}
		return "Int", nil, nil
	case "float32", "float64":
		return "Float", nil, nil
	}

	// No binding. Rejecting here closes the silent-`String` fallback.
	//
	// The old behaviour was to fall through as `String` so the schema at least
	// parsed, and nothing downstream caught it: the §26.5.2 walker-completeness
	// lint checks API-readable columns against walker case clauses, not scalar
	// resolution. A column bound to
	// an unrecognised Go type reached gqlgen as a `String` field in front of a
	// non-string Go field, which gqlgen answers with a panic("not implemented")
	// field resolver — outside the reach of the wrapper's Query/Mutation stub
	// rewriter — and by typing the generated input field `string`, which the
	// input translator does not compile against. The diagnostic therefore
	// arrived as a Go type error in generated code at the far end of a
	// multi-stage pipeline, naming neither the column nor the cause.
	//
	// inputCoercionFor rejects the writable half of this on its own, but only
	// the writable half: a read-only column never reaches it. This is the
	// check that covers every API-visible column, which is why both exist.
	//
	// PRD §26.4.1 already specifies this behaviour for the sibling case — "For
	// unknown types appearing in `api.graphql.scalars`, the consumer must
	// declare `marshaling`; codegen errors otherwise rather than silently
	// producing a non-compiling resolver."
	return "", nil, fmt.Errorf(
		"api: column %q resolves to Go type %q, which has no GraphQL binding; declare it under "+
			"`api.graphql.scalars` with a marshaling mode, or bind the column to a supported type "+
			"via `overrides.types` (PRD §26.4.1)",
		col.Name, goType,
	)
}

// setElemGoType returns the Go element type of a MySQL SET column's named
// slice, or "" when the column is not a SET *as resolved*. It is the single
// predicate every API-side SET decision keys on.
//
// The two halves of that sentence are why this is not just `col.IsSet`.
// ColumnContext.IsSet is derived from the SQL type NAME (context_table.go:
// `input.Resolver.IsSet(col.Type)`), but `type_map.<col>` and `overrides.types`
// win over the SET branch in the resolution chain — gotype.Resolve steps 1-4
// all precede the SET branch at step 4.5, and FromLiteral fills no element
// type. So a SET column the consumer retyped to `string` still carries
// IsSet=true while its Go type is not a named slice at all and has no element.
// Keying the list projection on IsSet alone advertised such a column as
// `[String!]!` in front of a `string` field and emitted `models.string(...)` in
// the input translator.
//
// SliceElemType is filled from the RESOLVED GoType, so it is the override-aware
// half of the pair, and empty is exactly the "not a named slice any more" case.
func setElemGoType(col ColumnContext) string {
	if !col.IsSet {
		return ""
	}
	return col.SliceElemType
}

// columnIsListShaped reports whether a column's GraphQL projection wraps its
// resolved type in a list.
//
// Two shapes qualify and they arrive differently. A PostgreSQL array column
// carries IsSlice, set by the gotype resolver. A MySQL SET is also a named
// slice, but the model side deliberately does NOT mark it IsSlice — there it
// is an opaque named type with its own Scan / Value and a String comparator
// (PRD §7.6), and flipping the flag would move pq.Array wrapping and comparator
// resolution with it. The API layer is the one place the two converge, because
// gqlgen sees the same `[<Enum>!]!` either way.
func columnIsListShaped(col ColumnContext) bool {
	return col.IsSlice || setElemGoType(col) != ""
}

// stripPointerAndSlice peels off `*` and `[]` prefixes so the registry lookup
// matches `uuid.UUID` against either `uuid.UUID` or `*uuid.UUID`.
func stripPointerAndSlice(goType string) string {
	t := goType
	for {
		switch {
		case strings.HasPrefix(t, "*"):
			t = strings.TrimPrefix(t, "*")
		case strings.HasPrefix(t, "[]"):
			t = strings.TrimPrefix(t, "[]")
		default:
			return t
		}
	}
}

// registerScalarUse adds a scalar to the global registry; the first
// registration wins on the (name, marshaling) tuple.
func registerScalarUse(use APIScalarUse, importPath string, scalarsByName map[string]APIScalarUse) {
	if existing, ok := scalarsByName[use.Name]; ok {
		// Prefer a non-empty import path if the existing entry has none.
		if existing.GoImport == "" && importPath != "" {
			existing.GoImport = importPath
			scalarsByName[use.Name] = existing
		}
		return
	}
	if use.GoImport == "" {
		use.GoImport = importPath
	}
	scalarsByName[use.Name] = use
}

// mapRelationshipToGraphQL converts a relationship into its GraphQL field shape.
// The shape is derived entirely from the first-class RelationshipContext.Type —
// never from the string spelling of GoType. Type fully determines
// both cardinality and nullability in the current codegen:
//   - OneToOne (o2o / m2o)    → `Target`     (nullable single)
//   - OneToMany / ManyToMany  → `[Target!]!` (non-null list of non-null elems)
//
// Every single relationship surfaces as a *nullable* GraphQL field: it is
// loaded lazily into a nilable pointer and may legitimately be absent — an
// unmatched FK, or a filtered config relationship whose predicate matches no
// row (e.g. Asset.PrimaryDocument, filtered on `entity_type = 'asset.primary'`).
// FK-column nullability (r.FKNullable) is deliberately NOT consulted: it
// describes the FK column, not whether the loaded relationship can be absent,
// which for filtered / lazily-loaded relationships it cannot guarantee.
//
// Dispatching on Type keeps the mapping refactor-proof: a change to how
// relationship fields are spelled (slice-of-values, dropping the pointer for a
// non-null FK, a named wrapper type) can no longer silently flip the GraphQL
// list or nullability shape.
func mapRelationshipToGraphQL(r RelationshipContext, casing string) APIRelationshipContext {
	out := APIRelationshipContext{
		SQLName:     r.Name,
		GraphQLName: graphQLFieldName(r.Name, casing),
		TargetType:  r.TargetStructName,
		GoFieldName: r.FieldName,
		Description: r.Description,
	}
	switch r.Type {
	case parser.OneToOne:
		out.GraphQLType = r.TargetStructName
	case parser.OneToMany, parser.ManyToMany:
		out.IsList = true
		out.GraphQLType = "[" + r.TargetStructName + "!]!"
	}
	return out
}

// graphQLFieldName converts a SQL identifier to the configured GraphQL casing.
// Both branches apply the §8.5 digit-leading guard so a column like
// `2010_revenue` produces a valid GraphQL identifier (`col2010Revenue` in
// camel mode, `col_2010_revenue` in snake mode) regardless of casing. The
// GraphQL spec requires identifiers to match `/[_A-Za-z][_0-9A-Za-z]*/`, so
// the raw SQL name cannot be passed through for digit-leading columns even
// when the user configured `snake_case`.
func graphQLFieldName(sqlName, casing string) string {
	if casing == config.FieldCasingSnake {
		return prefixIfDigitLeading(sqlName, "col_")
	}
	return toCamelCase(sqlName)
}

// collectComparatorFamilies returns every comparator input the shared schema
// must declare, in emission order: the shipped families in table order, each
// followed by its range input and its `Nullable<X>Comparator` twin, then any
// further family a column projection references, sorted by name.
//
// Emission is driven entirely by what columns actually reference (PRD §26.4
// Rule 2: "the Nullable variant of a family is emitted only when some
// nullable column actually references it"). That is not only a size
// optimisation — several families carry operands typed with a custom scalar
// (`Time`, `Decimal`) that is itself only declared when a column uses it, so
// an unconditionally-emitted input would reference an undeclared scalar and
// gqlgen would reject the schema. Deriving both from the same projections
// keeps the declaration and the reference in step by construction.
func collectComparatorFamilies(tables []APITableContext) []APIComparatorFamily {
	// The schema emits a comparator only for a non-PK filterable column, so
	// only those create a declaration obligation.
	referenced := make(map[string]bool)
	operatorsByName := make(map[string][]APIComparatorOperator)
	for _, t := range tables {
		for _, f := range t.Fields {
			if f.PrimaryKey || !f.Filterable || f.Filter.InputTypeName == "" {
				continue
			}
			referenced[f.Filter.InputTypeName] = true
			operatorsByName[f.Filter.InputTypeName] = f.Filter.Operators
		}
	}

	out := make([]APIComparatorFamily, 0, len(shippedComparatorFamilies))
	emitted := make(map[string]bool, len(referenced))
	add := func(fam APIComparatorFamily) {
		fam.NameWidth = comparatorNameWidth(fam.Operators)
		emitted[fam.Name] = true
		out = append(out, fam)
	}

	for _, base := range shippedComparatorFamilies {
		twin := nullableComparatorTwin(base)
		wantBase, wantTwin := referenced[base.Name], referenced[twin.Name]
		if !wantBase && !wantTwin {
			continue
		}
		if wantBase {
			add(base)
		} else if base.RangeInputName != "" {
			// The range input is declared alongside the base form. When only
			// the twin is referenced the base block is not emitted, so carry
			// the range declaration onto the twin instead — `between` /
			// `nbetween` name it from either form.
			twin.RangeInputName = base.RangeInputName
			twin.RangeOperandType = base.RangeOperandType
		}
		if wantTwin {
			add(twin)
		}
	}

	extra := make([]APIComparatorFamily, 0)
	for name := range referenced {
		if emitted[name] {
			continue
		}
		extra = append(extra, APIComparatorFamily{
			Name:      name,
			Operators: operatorsByName[name],
		})
	}
	slices.SortFunc(extra, func(a, b APIComparatorFamily) int {
		return strings.Compare(a.Name, b.Name)
	})
	for _, fam := range extra {
		add(fam)
	}
	return out
}
