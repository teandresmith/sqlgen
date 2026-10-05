// Package gen provides the code generation engine: template execution,
// context building, and file writing.
//
// Context structs hold all pre-computed data for template rendering.
// Templates iterate over these structs — they never query the schema
// or config directly. See the context_*.go files for builders.
package gen

import (
	"slices"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/parser"
)

// --- Generation input ---

// GenerateInput bundles all inputs needed by context builders.
type GenerateInput struct {
	Schema   *parser.Schema
	Config   *config.RootConfig
	Resolver *gotype.Resolver

	// Warnings collects non-fatal notices raised while building contexts, in
	// build order. Generate copies them into GenerateResult so the CLI can
	// print them alongside the config-validation warnings. The build path is
	// sequential (BuildTableContexts loops tables in order), so appending
	// needs no synchronization.
	Warnings []string

	// relTargets resolves a relationship's target to the entity generated for
	// it. BuildTableContexts sets it before building any table.
	relTargets *relationshipTargets
}

// --- Context struct definitions ---

// EnumContext holds pre-computed data for rendering a single enum template.
type EnumContext struct {
	// Name is the SQL enum name (e.g., "order_status").
	Name string
	// Schema is the SQL schema name (e.g., "public").
	Schema string
	// GoTypeName is the PascalCase Go type name (e.g., "OrderStatus").
	GoTypeName string
	// SliceGoTypeName is the named slice type (e.g., "OrderStatusSlice").
	// Only set for PostgreSQL enums; empty for other dialects.
	SliceGoTypeName string
	// Values is the ordered list of enum values.
	Values []string
	// DocComment is the SQL COMMENT ON TYPE value, if any.
	DocComment string
	// FromSet marks a context synthesized from a MySQL SET type rather than
	// read from a schema enum (enumContextsForSets). The two share this shape
	// because the API surface projects them identically, but they diverge on
	// everything a SET does not have: no `enums:` config entry renames it
	// (BuildSetContexts owns its naming), and it has no monomorphized
	// comparator — a SET column filters through comparator.String.
	FromSet bool
}

// CompositeTypeContext holds pre-computed data for a composite type.
type CompositeTypeContext struct {
	// Name is the SQL composite type name.
	Name string
	// Schema is the SQL schema name.
	Schema string
	// GoTypeName is the PascalCase Go type name.
	GoTypeName string
	// Fields holds the typed attributes.
	Fields []CompositeFieldContext
	// DocComment is the SQL COMMENT ON TYPE value, if any.
	DocComment string
	// FromSet marks a context synthesized from a MySQL SET type rather than
	// read from a schema enum (enumContextsForSets). The two share this shape
	// because the API surface projects them identically, but they diverge on
	// everything a SET does not have: no `enums:` config entry renames it
	// (BuildSetContexts owns its naming), and it has no monomorphized
	// comparator — a SET column filters through comparator.String.
	FromSet bool
}

// CompositeFieldContext holds one attribute of a composite type.
type CompositeFieldContext struct {
	// FieldName is the PascalCase Go field name.
	FieldName string
	// GoType is the resolved Go type expression.
	GoType string
	// Import is the import path for the Go type.
	Import string
	// JSONTag is the json struct tag value (SQL attribute name).
	JSONTag string
}

// DomainTypeContext holds pre-computed data for a domain type.
type DomainTypeContext struct {
	// Name is the SQL domain type name.
	Name string
	// Schema is the SQL schema name.
	Schema string
	// GoTypeName is the PascalCase Go type name.
	GoTypeName string
	// BaseGoType is the resolved Go type for the underlying SQL type.
	BaseGoType string
	// Import is the import path for the base Go type.
	Import string
	// DocComment is the SQL COMMENT ON DOMAIN value, if any.
	DocComment string
}

// ExtraTypeContext holds pre-computed data for a config-defined extra type.
type ExtraTypeContext struct {
	// GoTypeName is the PascalCase Go type name.
	GoTypeName string
	// Description is the optional doc comment.
	Description string
	// Fields holds the typed fields, sorted by name.
	Fields []ExtraFieldContext
}

// ExtraFieldContext holds one field of an extra type.
type ExtraFieldContext struct {
	// FieldName is the PascalCase Go field name.
	FieldName string
	// GoType is the Go type expression.
	GoType string
	// Import is the import path for the Go type.
	Import string
	// Description is the optional doc comment.
	Description string
	// Tags holds struct tag key-value pairs, sorted by key.
	Tags []TagPair
}

// TagPair is a single struct tag key-value pair.
type TagPair struct {
	Key   string
	Value string
}

// TypeContext holds all non-table type contexts for a generation run.
type TypeContext struct {
	Composites []CompositeTypeContext
	Domains    []DomainTypeContext
	Extras     []ExtraTypeContext
}

// ColumnContext holds pre-computed data for a single table column.
type ColumnContext struct {
	// Name is the SQL column name.
	Name string
	// FieldName is the Go struct field name: PascalCase of Name, or the
	// `column_map.<col>.name` override when one is set (PRD §8.5).
	FieldName string
	// FieldNameOverridden reports whether FieldName came from
	// `column_map.<col>.name` rather than from the naming engine. Only the
	// gqlgen handoff reads it: a renamed field on a *bound* row type needs an
	// explicit `models.<T>.fields.<f>.fieldName` entry, because gqlgen would
	// otherwise bind by case-insensitive comparison against the name it
	// derives from the GraphQL field (PRD §26.5.6).
	FieldNameOverridden bool
	// GoType is the resolved Go type expression (e.g., "string", "*int32", "uuid.UUID").
	GoType string
	// Import is the import path for the Go type (empty for builtins).
	Import string
	// ZeroValue is the Go zero value expression for the type.
	ZeroValue string
	// SQLType is the original SQL type string.
	SQLType string
	// BaseSQLType is SQLType with domain definitions followed to the
	// underlying base (`email` → `text`) and case normalized. Equal to
	// SQLType for every column that is not domain-typed. Surfaces that
	// *classify* a column — increment eligibility, notably — must read this
	// rather than SQLType, since a domain name belongs to no category.
	BaseSQLType string
	// Nullable indicates whether the column allows NULL.
	Nullable bool
	// PrimaryKey indicates whether the column is part of the primary key.
	PrimaryKey bool
	// HasDefault indicates whether the column has a DEFAULT expression.
	HasDefault bool
	// DefaultExpr is the raw SQL default expression for this column.
	// For columns with an explicit DEFAULT, this is the expression (e.g., "datetime('now')").
	// For nullable columns without a DEFAULT, this is "NULL".
	// Empty when the column is neither nullable nor has a default.
	DefaultExpr string
	// AutoIncrement indicates serial/auto_increment columns.
	AutoIncrement bool
	// IsComputed indicates GENERATED ALWAYS columns.
	IsComputed bool
	// Description is the resolved doc comment (config > SQL comment > "").
	Description string
	// DBTag is the `db` struct tag value (column name).
	DBTag string
	// JSONTag is the `json` struct tag value (column name).
	JSONTag string
	// IsSlice indicates Go slice types (PostgreSQL arrays).
	IsSlice bool
	// SliceElemType is the element type for named slices: PostgreSQL enum
	// arrays (e.g., "UserRole") and MySQL SETs (e.g., "UsersPermissionsSetValue").
	// Empty for bare slices like []string or []uuid.UUID.
	SliceElemType string
	// IsSet indicates MySQL SET types (named []ValueType slices).
	IsSet bool
	// FKConvert indicates how to convert the value to string for FK comparisons.
	FKConvert gotype.FKStringMethod
	// FKReference points to the referenced table/column, or nil.
	FKReference *FKReferenceContext
	// Unique indicates a single-column UNIQUE constraint.
	Unique bool
	// Tenant reports that this column is the entity's resolved tenant column
	// (PRD §29.2.3). Populated by attachTenancyToTables / attachTenancyToViews
	// after the entity context is assembled, so it is false on a context built
	// by BuildTableContexts alone — see the wiring rule in
	// guidelines/TEMPLATES.md §6. Views carry it only on the full Generate
	// path: BuildEntityContextsFromSchema attaches tenancy to tables only.
	// Read by isIncrementEligible: a row's tenant is an identity, not an
	// amount (PRD §8.2, §29.4.2).
	Tenant bool
	// Access is the resolved access role (PRD §32.2): "public", "read_only",
	// "write_only", "hidden", or "internal". Unset config values resolve to
	// "public". Access never narrows the core Go client — only the generated
	// API, event payloads, and manifest consume it (via the capability
	// fields below).
	Access string
	// APIReadable: the column appears in the GraphQL object type (API out).
	APIReadable bool
	// APIWritable: the column appears in the API create/update inputs.
	APIWritable bool
	// APIFilterable: the column appears in the API filter input (subject to
	// the column type having a comparator — access only removes, never adds).
	APIFilterable bool
	// APISortable: the column appears in the API sort enum.
	APISortable bool
	// EventRedacted: the column is cleared from the published Event.Input clone.
	EventRedacted bool
	// ManifestVisibility: the access marker emitted on the manifest column.
	ManifestVisibility string
}

// FKReferenceContext holds foreign key reference information.
type FKReferenceContext struct {
	Table  string
	Schema string
	Column string
	// Synthetic mirrors parser.FKReference.Synthetic: the reference was
	// invented by a config-declared relationship rather than read from a SQL
	// REFERENCES clause, so the database does not enforce it. PK strategy
	// detection reads it — see autoDetectPKStrategy.
	Synthetic bool
}

// SoftDeleteContext holds soft delete detection results for a table.
type SoftDeleteContext struct {
	// Column is the soft delete column name (e.g., "deleted_at").
	Column string
	// FieldName is the Go field name for Column on the entity and filter
	// structs (e.g., "DeletedAt"). Resolved from the column context rather
	// than re-derived in templates, so a `column_map.<col>.name` override is
	// honoured wherever the soft-delete field is referenced.
	FieldName string
	// Strategy is the soft delete type: "timestamp", "bool", or "integer".
	Strategy config.SoftDeleteType
}

// UpdateColumnContext pairs an auto-set update column's SQL name with the Go
// field name it carries on the update input. Both spellings are needed by the
// update template — the SQL name keys the SET clause, the field name reads the
// caller's omittable value — and deriving the second from the first in the
// template would bypass any `column_map.<col>.name` override.
type UpdateColumnContext struct {
	// Name is the SQL column name.
	Name string
	// FieldName is the Go field name on the update input struct.
	FieldName string
}

// RelationshipContext holds pre-computed data for a table relationship.
type RelationshipContext struct {
	// Name is the relationship name.
	Name string
	// Type is the relationship type (OneToOne, OneToMany, ManyToMany).
	Type parser.RelationshipType
	// Side identifies which end of the edge this entry represents. Used by
	// the manifest builder to disambiguate o2o (parent / FK-holder) from m2o
	// (child / referenced) — both carry Type = OneToOne. Zero value
	// (SideUnspecified) signals "no explicit side"; consumers fall back to
	// their legacy heuristic in that case.
	Side parser.RelationshipSide
	// TargetTable is the SQL name of the related table.
	TargetTable string
	// TargetSchema is the SQL schema of the related table (e.g., "public", "audit").
	TargetSchema string
	// TargetStructName is the Go struct name of the related table.
	TargetStructName string
	// FKColumn is the foreign key column name (O2O/O2M).
	FKColumn string
	// FKFieldName is the Go field name for the FK column (e.g., "ProductID").
	// The FK column lives on the *target* table for O2M edges, so this is
	// resolved cross-table in wireRelationshipFKMetadata rather than derived
	// from FKColumn here — otherwise a `column_map.<col>.name` override on
	// the target table would not be picked up.
	//
	// Which column it names follows the same table the rest of the FK
	// metadata is read from: the target table's FKColumn for O2M/O2O, and
	// the junction's JunctionLocalFK for M2M — where FKColumn is empty on
	// every auto-detected edge (parser.addM2M sets only the junction FKs),
	// so there was nothing to derive from before. Only get.go.tmpl's O2M
	// loader consumes this today.
	FKFieldName string
	// FKOnTarget reports whether FKColumn is physically declared on the
	// *target* table rather than on this one (PRD §13.1). It is what separates
	// the two O2O shapes `type: one_to_one` cannot tell apart: a belongs-to
	// edge holds the FK here (false), a has-one edge holds it on the related
	// table (true). O2M resolves true — the FK is on the many side — and M2M
	// resolves false, because a junction edge carries no FK on either end.
	// An edge whose target is not in the parsed schema also resolves false, so
	// false alone does not mean belongs-to; see fkColumnOnTarget.
	//
	// It is *not* the Side field. `side` is declared, defaults to "parent" and
	// drives the manifest's o2o / m2o split; a config edge can leave it at that
	// default and still put the FK on the target, which is exactly the shape of
	// `assets.PrimaryDocument` in the graphql example.
	//
	// Derived once, when the relationship context is built, from the parsed
	// schema — so the read path (the O2O LEFT JOIN's ON clause, which used to
	// re-derive it by scanning the target's columns) and the write path consume
	// one value rather than each deriving their own. The cross-table pass in
	// wireRelationshipFKMetadata asks a related but distinct question — which
	// *generated* table context carries the column, so it can read that
	// column's resolved nullability and Go type — and is not a second
	// derivation of this one.
	FKOnTarget bool
	// JunctionTable is the bare junction table name without schema prefix (M2M).
	JunctionTable string
	// JunctionSchema is the schema of the junction table (M2M).
	JunctionSchema string
	// JunctionLocalFK is the local FK in the junction table (M2M).
	JunctionLocalFK string
	// JunctionReferenceFK is the reference FK in the junction table (M2M).
	JunctionReferenceFK string
	// Filter is a polymorphic filter expression.
	Filter string
	// Discriminator is the structured form of the same predicate (PRD
	// §13.4.1), or nil. Mutually exclusive with Filter — config validation
	// rejects an edge declaring both — so at most one of the two is ever set.
	Discriminator *DiscriminatorContext
	// Sort defines static ordering for loaded related entities. Only the
	// O2M / M2M loaders read it, through the client's <rel>DefaultSort field;
	// config validation rejects it on an O2O edge (PRD §4.8, §13.2).
	Sort []SortContext
	// FieldName is the Go field name for the relationship on the model struct.
	FieldName string
	// GoType is the Go type expression (e.g., "*Order" or "[]*OrderItem").
	GoType string
	// Description is the doc comment for the relationship field (e.g., "one-to-one relationship with the companies table.").
	Description string
	// JSONTag is the json struct tag value (snake_case relationship name).
	JSONTag string
	// FKGoType is the Go type of the FK column (e.g., "int64", "string").
	// For O2M: the FK column on the child table (matches parent PK type).
	// For M2M: the target table's PK type.
	// Used by templates to generate the correct comparator type for FK/PK filtering.
	FKGoType string
	// FKNullable indicates whether the FK column is nullable.
	// For O2M/O2O: nullability of the FK column on the target/child table.
	// For M2M: nullability of the junction table's local FK column.
	// Templates use this to choose between `comparator.Number`/`comparator.ID`
	// and `comparator.NullableNumber`/`comparator.NullableID` when emitting
	// relationship-load filter expressions.
	FKNullable bool
	// FKColumnGoType is the resolved Go type of the FK column itself (which
	// may differ from FKGoType when the FK is nullable and bound to a
	// Null-wrapper struct, e.g. uuid.NullUUID under the uuid integration).
	// Used by the O2M relationship loader to emit the correct
	// guard + unwrap expression when stringifying a FK for bucket lookup.
	// For M2M: the local junction FK's Go type.
	FKColumnGoType string
	// TargetPKFieldName is the Go field name of the target table's first PK
	// column (e.g. "ID"), and TargetPKColumn is that column's SQL name (e.g.
	// "id"). Only M2M consumes them: the loader maps loaded targets back to
	// their parents through a map keyed on the field, and declares the column
	// on the target fetch's requiredColumns so a narrowed projection cannot
	// leave the key at its zero value. Filled cross-table by
	// wireRelationshipFKMetadata; the field name falls back to the parent's PK
	// spelling when the target is outside the generated set (unit fixtures,
	// bare tables).
	TargetPKFieldName string
	TargetPKColumn    string
}

// SortContext holds a single sort clause.
type SortContext struct {
	Column string
	// Direction is "ASC" or "DESC", normalized from config by sortDirection.
	Direction string
}

// ConflictTargetContext holds a single conflict target for upsert.
type ConflictTargetContext struct {
	// ConstantName is the Go constant name (e.g., "ProductConflictSKU").
	ConstantName string
	// Columns is the set of columns forming the conflict target.
	Columns []string
	// Comment describes the constraint origin (e.g., "PRIMARY KEY (id)").
	Comment string
	// CoversPK reports whether Columns includes every primary-key column.
	//
	// It is what decides where a batched upsert gets its written keys (PRD
	// §9.5). Only a PK-covering target makes them caller-known: the conflict
	// then fired on columns that include the key, so the row the statement
	// touched is the row carrying the key the input supplied. On any other
	// target the conflicting row keeps its **own** key — the PK column is
	// excluded from the update half of the clause — so a key taken from the
	// input names a row that does not exist. Such a target must read its keys
	// back instead.
	//
	// The single-row Upsert does not need this: it resolves through RETURNING
	// or resolveUpsertConflictRow on every shape that could be wrong. Batched,
	// neither mechanism is available (PRD §9.7), so the distinction has to be
	// carried explicitly.
	CoversPK bool
}

// RelationshipOptionsDef holds the definition of a RelationshipOptions struct
// generated for O2M/M2M relationships (e.g., ReviewRelationshipOptions).
type RelationshipOptionsDef struct {
	// StructName is the RelationshipOptions struct name (e.g., "ReviewRelationshipOptions").
	StructName string
	// TargetStructName is the related table struct name (e.g., "Review").
	TargetStructName string
}

// DiscriminatorContext carries a relationship's structured polymorphic
// predicate (PRD §13.4.1) in the two renderings the read paths need.
//
// Two renderings, because the three read paths do not share a parameter
// channel. The O2M / M2M loader and the relationship-filter EXISTS subquery
// both bind the value — `sql.Raw` carries args, and the subquery has its own
// `subArgs` slice — so they use GoLiteral against a "$" token. The O2O path
// compiles into [sql.JoinClause.On], which is documented opaque SQL that
// builders "do not parse or number placeholders inside"
// (`sql/builder.go:80-87`), an invariant tenancy's placeholder ordering
// depends on; it therefore interpolates SQLLiteral, exactly as the equivalent
// `filter:` did.
//
// Column is quoted through the dialect on the two bound paths, which construct
// the predicate themselves and so own the identifier. The O2O path leaves it
// bare to stay byte-identical with the `filter:` form it replaces.
type DiscriminatorContext struct {
	// Column is the discriminator column on the *target* table. Config
	// validation has already confirmed it exists there (PRD §4.13).
	Column string
	// GoLiteral is the value as a Go source literal, bound as a parameter by
	// the paths that can carry args.
	GoLiteral string
	// SQLLiteral is the value as a SQL literal, interpolated by the O2O JOIN
	// path. String values are single-quoted with embedded quotes doubled.
	SQLLiteral string
}

// RelationshipFilterContext holds everything the filter template needs to
// compile one list relationship into a correlated EXISTS predicate (PRD
// §11.1). Only O2M and M2M relationships contribute an entry — a to-one
// relationship's FK column is directly filterable on the parent, so v1 emits
// no member for it.
//
// The target-side facts (PK column, soft delete, tenant column) live on the
// *target's* TableContext, so they are filled in by wireRelationshipFilters
// after every table context exists, the same cross-table pass shape
// wireRelationshipFKMetadata uses.
type RelationshipFilterContext struct {
	// FieldName is the Go field name on the filter struct (e.g. "Posts").
	FieldName string
	// SQLName is the relationship's declared name (e.g. "order_items"), the
	// same string mapRelationshipToGraphQL derives the object type's field
	// name from. Carried so the GraphQL filter member and the GraphQL
	// relationship field are spelled by one derivation (PRD §26.4) — a
	// relationship exposed as `orderItems` on the type must be `orderItems`
	// inside the filter input too.
	SQLName string
	// JSONTag is the snake_case json tag for the member.
	JSONTag string
	// TargetStructName is the target's Go struct name (e.g. "Post"), which
	// also spells the member's type: *PostFilter.
	TargetStructName string
	// TargetTable / TargetSchema name the related table.
	TargetTable  string
	TargetSchema string
	// HelperName is the generated compile function (e.g. "userPostsFilterExists").
	HelperName string
	// CorrelationColumn is the parent-side column the subquery correlates on —
	// the parent's single PK column. Emitted without a qualifier: the builder
	// resolves it per read path (PRD §11.5).
	CorrelationColumn string
	// FKColumn is the target-side FK for an O2M edge. Empty for M2M.
	FKColumn string
	// IsM2M selects the junction shape: FROM junction JOIN target, correlating
	// on the junction's local FK.
	IsM2M bool
	// JunctionTable / JunctionSchema / JunctionLocalFK / JunctionReferenceFK
	// describe the M2M junction. Empty for O2M.
	JunctionTable       string
	JunctionSchema      string
	JunctionLocalFK     string
	JunctionReferenceFK string
	// TargetPKColumn is the target's single PK column, joined to
	// JunctionReferenceFK. Only set for M2M.
	TargetPKColumn string
	// Discriminator is the relationship's config `discriminator:` predicate
	// (PRD §13.4.1), or nil. Mutually exclusive with StaticFilterExpr.
	//
	// Unlike StaticFilterExpr this is not pre-qualified: the subquery builds the
	// predicate itself from Column and GoLiteral, so it quotes the identifier
	// through the dialect and binds the value into subArgs rather than
	// splicing a literal into the SQL. Only O2M and M2M edges reach this path
	// — buildRelationshipFilters draws from those two lists — so every
	// discriminator here binds, with no O2O exception to carry.
	Discriminator *DiscriminatorContext
	// StaticFilterExpr is the Go expression that rebuilds the relationship's
	// config `filter:` predicate (PRD §13.7.1) with the subquery's runtime
	// alias spliced in. AND-ed into the subquery so a sub-categorized
	// relationship filter cannot match another sub-category's rows. Empty when
	// the relationship declares no `filter:`.
	//
	// It is an expression rather than the qualified SQL because the alias is
	// depth-dependent and unknown until the filter is compiled, and a context
	// field rather than a funcmap call because deriving it needs the dialect
	// and an error channel (see relationshipStaticFilter).
	StaticFilterExpr string
	// SoftDelete is the target's soft delete column, or nil when the target
	// has none or its exclude_deleted is off. Non-nil means the subquery
	// injects the default "is not deleted" predicate unless the caller set
	// the comparator on the target filter (PRD §17.3).
	SoftDelete *SoftDeleteContext
	// TenantColumn is the target's tenant SQL column, or "" when the target is
	// not tenanted. Non-empty means the subquery injects the resolved tenant
	// predicate so a relationship filter cannot probe another tenant
	// (PRD §11.1 invariant 1, §29.4.1).
	TenantColumn string
}

// FilterFieldContext holds the comparator mapping for a single column in a filter struct.
type FilterFieldContext struct {
	// FieldName is the Go field name.
	FieldName string
	// ComparatorType is the comparator type expression (e.g., "comparator.String", "comparator.Number[int64]").
	// Empty when Filterable is false.
	ComparatorType string
	// ComparatorImport is the import path for the comparator package.
	ComparatorImport string
	// ColumnName is the SQL column name.
	ColumnName string
	// IsSoftDeleteColumn marks whether this filter field corresponds to the soft delete column.
	IsSoftDeleteColumn bool
	// Filterable reports whether the column should appear in the generated
	// filter struct. False for columns whose Go type cannot satisfy the
	// comparator package's `T comparable` constraint (e.g. `json[]` / `jsonb[]`
	// resolving to `[]types.JSON`), and for a JSON column on SQLite,
	// whose comparator family has no arm for that dialect. Templates
	// that range over FilterFields must skip entries where Filterable is false.
	Filterable bool
}

// InputFieldContext holds pre-computed data for a field in a Create/Update input struct.
type InputFieldContext struct {
	// FieldName is the PascalCase Go field name.
	FieldName string
	// GoType is the Go type expression for this input field.
	GoType string
	// Import is the import path for the Go type.
	Import string
	// ColumnName is the SQL column name.
	ColumnName string
	// Required indicates the field must be provided (bare type in CreateInput).
	Required bool
	// Omittable indicates the field uses omittable.Value[T].
	Omittable bool
	// Description is the optional doc comment.
	Description string
	// JSONTag is the json struct tag value (column name).
	JSONTag string
	// DefaultExpr is the raw SQL default expression for this column, used
	// by dialects that do not support the DEFAULT keyword in VALUES (SQLite).
	// Contains the expression (e.g., "datetime('now')") or "NULL" for nullable
	// columns without a default. Empty when the column has no fallback.
	DefaultExpr string
}

// ScanShapeContext holds scan target information for a column.
type ScanShapeContext struct {
	// ColumnName is the SQL column name.
	ColumnName string
	// FieldName is the Go field name.
	FieldName string
	// Shape is one of "direct", "wrapped".
	Shape string
	// ScanExpr is the scan target expression (e.g., "&p.Name", "pq.Array(&p.Tags)").
	ScanExpr string
}

// ResolvedOperations is a table's client method set: one flag per method
// group. The client has no operations toggle (PRD §4.6), so on a table context
// every flag is a schema fact — toResolvedOperations, then the cursor-key and
// conflict-target gates, then wireNestedMutations for the three nested flags.
// The API surface reuses the type for the same set intersected with the
// `api.operations` mask (maskAPIOperations).
type ResolvedOperations struct {
	Get         bool
	GetMany     bool
	Create      bool
	CreateMany  bool
	Update      bool
	UpdateMany  bool
	UpdateWhere bool
	Upsert      bool
	UpsertMany  bool
	SoftDelete  bool
	Restore     bool
	HardDelete  bool
	Exists      bool
	Count       bool
	Increment   bool
	Paginate    bool
	Connection  bool
	Stream      bool
	// The three nested-mutation methods (PRD §9.9): true exactly when the
	// method is emitted, which `nested_mutations` and edge eligibility alone
	// decide (wireNestedMutations).
	CreateWithRelated bool
	UpdateWithRelated bool
	UpsertWithRelated bool
}

// NestedContext holds everything one parent table's nested-mutation surface
// needs (PRD §9.9). It is nil on every table that carries no eligible edge:
// a wrapper holding only the flat input would be the base operation with
// extra steps, so such a parent emits no method, no wrapper type and no
// GraphQL mutation.
//
// Built by wireNestedMutations, a post-build pass: eligibility reads the
// *target's* primary key, access projection and tenancy, so every table
// context has to exist and the attach passes have to have run first.
type NestedContext struct {
	// CreateInputName is `Create<Parent>WithRelatedInput`, UpdateInputName
	// `Update<Parent>WithRelatedInput` and UpsertInputName
	// `Upsert<Parent>WithRelatedInput` (PRD §9.9.5). All three are claimed by
	// the name registry whenever the parent has a candidate edge, so they are
	// spelled here whether or not the matching operation is emitted.
	CreateInputName string
	UpdateInputName string
	UpsertInputName string
	// EmitCreate / EmitUpdate / EmitUpsert gate the three methods. Each is the
	// intersection of the `generation.nested_mutations.operations` mask (PRD
	// §4.6), the table's own resolved `<x>_with_related` operation, and — for
	// upsert alone — the upsert conflict-target rule: a parent that emits no
	// `<Parent>ConflictTarget` constant has nothing the caller could pass as
	// the target argument, so the nested upsert surface is omitted with it
	// (PRD §9.9.4, §9.5).
	//
	// They live here rather than being recomposed in each template because
	// three templates read them — the client interface, the method bodies and
	// the manifest builder — and a family emitted by one and not another is a
	// package that does not compile.
	EmitCreate bool
	EmitUpdate bool
	EmitUpsert bool
	// ParentFieldOptionsFunc is the per-parent helper that strips relationship
	// members off the caller's selection before the parent write, and
	// SelectsRelationshipFunc the one that decides whether the terminal
	// re-read runs.
	ParentFieldOptionsFunc  string
	SelectsRelationshipFunc string
	// RelationshipFieldNames is every relationship member on the parent's
	// FieldOptions struct — not just the nestable ones. The parent write
	// strips all of them (PRD §9.9.6), and the terminal read fires when the
	// caller selected any of them.
	RelationshipFieldNames []string
	// Edges are the eligible (parent, edge) pairs in byte-wise order of the
	// relationship's Go field name, which is the order the members are declared in and
	// the order the executors run in (PRD §9.9.5).
	Edges []NestedEdgeContext
}

// NestedEdgeContext is one eligible (parent, edge) pair — the per-edge verb
// block, the nested child input, and every pre-computed expression the
// executor needs (PRD §9.9).
type NestedEdgeContext struct {
	// FieldName is the relationship's Go field name on the parent struct. It
	// is also the edge spelling in every error (PRD §9.9.8 / §22.2) and the
	// member name on the wrapper input.
	FieldName string
	// JSONTag is the snake_case member tag.
	JSONTag string
	// Description is the doc line for the emitted member.
	Description string
	// Shape is "has_one", "o2m" or "m2m" — PRD §9.9.1's shapes 2, 3 and 4.
	// Shape 1 (belongs-to) never reaches here.
	Shape string
	// SelfReferential reports that the edge's target is the parent table
	// itself (PRD §13.5). It adds exactly one rule — the refusal of a
	// `connect` naming the parent's own primary key, which no other guard
	// catches and which writes a row that is its own parent.
	SelfReferential bool
	// TargetResolvesTenant reports that a client this edge writes through
	// resolves a tenant — the target, or on an M2M the junction the link step
	// writes. It feeds the parent's single-resolve hoist (PRD §29.6, §29.10),
	// and it is a fact about the *callee*, so a shared parent with a
	// tenanted edge sets it just as a tenanted one does.
	//
	// It reads the target's resolver need, not its tenant column: a shared
	// target that appends a tenanted O2O child's filter, or compiles a
	// relationship filter against a tenanted table, resolves a tenant inside
	// this write too — the `connect` visibility read goes through filterOptions.
	// Gating on the column alone left those edges paying a second resolve.
	TargetResolvesTenant bool
	// Imports are the import paths the emitted executor needs beyond the
	// parent's own. Under the file_per_table layout the parent's file carries
	// only its own columns' imports, and an executor names three types that
	// come from elsewhere: the nested child input's fields, the target's
	// primary key (the connect list, the dedupe set, the owner map's key) and
	// the traversed FK's column type (the owner map's value). An M2M edge has
	// no child fields at all, so collecting only those under-collects exactly
	// where the gap bites.
	Imports []string
	// CreateBlockName is `<Parent><Edge>CreateNested` and UpdateBlockName
	// `<Parent><Edge>UpdateNested` — the create-side block is the narrower
	// type, carrying `create` and `connect` alone, because `disconnect` and
	// `clear` are meaningless on a row that does not exist yet and must not be
	// expressible rather than accepted and ignored (PRD §9.9.5).
	//
	// ChildInputName is `<Parent><Edge>CreateInput` (empty on M2M, which takes
	// the target's own create input unchanged), and ExecutorName is the
	// per-edge executor all three families call. The executor takes
	// the *update-side* block, and CreateWithRelated widens its create-side
	// block at the call — which is sound precisely because `Disconnect` and
	// `Clear` are absent from that type, so the widening cannot smuggle a verb
	// in.
	CreateBlockName string
	UpdateBlockName string
	ChildInputName  string
	ExecutorName    string
	// WidenFuncName is the per-edge helper CreateWithRelated calls to lift its
	// create-side block into the update-side one the executor takes.
	WidenFuncName string

	// --- target ---

	// TargetStructName / TargetPlural spell the target's own generated types.
	TargetStructName string
	TargetPlural     string
	// TargetClientField is the entity-client field the writes route through,
	// e.g. "eventClient" (PRD §9.9's "every write routes through the target's
	// own generated client").
	TargetClientField string
	// TargetPKFieldName / TargetPKGoType name the target's single PK column.
	TargetPKFieldName string
	TargetPKGoType    string
	// TargetPKIsString reports whether the target's PK filters through
	// comparator.ID (a []string `In`) rather than comparator.Number /
	// comparator.Opaque, which take the key's own Go type. ConnectKeyGoType
	// and ConnectKeyExpr below are the rendered form of that decision.
	TargetPKIsString bool

	// --- create verb ---

	HasCreate bool
	// ChildCreateType is the element type of the `create` verb: the nested
	// child input on shapes 2 and 3, `Create<Target>Input` on M2M.
	ChildCreateType string
	// ChildFields are the target's create-input fields that survive into the
	// nested child input — every field except the traversed FK and the edge's
	// discriminator column (PRD §9.9.5). Empty on M2M.
	ChildFields []InputFieldContext
	// FKFieldName is the traversed FK's Go field on the target's create input,
	// and FKAssignExpr the right-hand side that sets it from the parent's key
	// (PRD §9.9.6). Empty on M2M, where the link lives in the junction row.
	FKFieldName  string
	FKAssignExpr string
	// DiscFieldName / DiscAssignExpr set the edge's discriminator column on
	// every nested create. Empty when the edge declares none.
	DiscFieldName  string
	DiscAssignExpr string
	// DiscColumn / DiscGoLiteral carry the same predicate to the `connect`
	// visibility read, so a target whose discriminator does not match
	// the edge is ErrNotFound rather than a silent mislink. Spelled through
	// the same `conditions` escape and the same dialect-quoted, parameter-bound
	// form the O2M read loader uses, so the two cannot drift.
	DiscColumn    string
	DiscGoLiteral string

	// --- connect verb ---

	HasConnect bool
	// ConnectIDGoType is the Go type of one entry in the `connect` list — the
	// target's PK type.
	ConnectIDGoType string
	// ConnectKeyGoType is the element type the target's PK comparator takes:
	// `string` for the comparator.ID family, the key's own Go type for
	// comparator.Number and comparator.Opaque (PRD §11.2). ConnectKeyExpr
	// converts one list entry, spelled `v`, into that element.
	ConnectKeyGoType string
	ConnectKeyExpr   string
	// ConnectFilterExpr and AdoptFilterExpr are the id `IN` terms of the
	// visibility read and of the adoption UPDATE, over the locals `ids` and
	// `adopt` the executor builds. ConnectFilterExpr also spells the
	// `disconnect` statement's `IN` term, which reads the same `ids` local in
	// its own scope — one term, because it is the same question asked of the
	// same key.
	ConnectFilterExpr string
	AdoptFilterExpr   string
	// AllowReparent lets a connect adopt a target already parented elsewhere
	// (PRD §4.8 / §9.9.6). O2M and has-one only; an M2M connect adds a link
	// rather than moving one.
	AllowReparent bool
	// FKFilterField / FKFilterNullExpr compile the adoption UPDATE's `IS NULL`
	// guard — the target's FK filter member and the term that makes the
	// adoption atomic against a concurrent connect. FKFilterNullExpr is empty
	// under AllowReparent, which deliberately adopts without the guard
	// (PRD §9.9.6); FKFilterField is not, because the unlink verbs and the
	// adoption's verify read filter on the same member with ParentFilterExpr.
	// Both are empty on M2M.
	FKFilterField    string
	FKFilterNullExpr string
	// FKOwnerGoType / FKOwnerGuard / FKOwnerKey read the FK back off a
	// visibility-read row: the model column's Go type, the non-null guard, and
	// the unwrapped value to compare against the parent's key. A nullable FK
	// is always a pointer or a Null wrapper, so the guard is never empty.
	FKOwnerGoType string
	FKOwnerGuard  string
	FKOwnerKey    string
	// FKUpdateAssignExpr sets the FK to the parent's key through the target's
	// update input.
	FKUpdateAssignExpr string

	// --- disconnect and clear verbs ---

	HasDisconnect bool
	HasClear      bool
	// ParentKeyExpr converts the parent's primary key into the element type
	// the parent-scoped filter term takes — a string on the comparator.ID
	// family, the key's own Go type on comparator.Number / comparator.Opaque
	// (PRD §11.2). The emitted executor binds it to a local named `pid`, which
	// ParentFilterExpr then reads.
	ParentKeyExpr string
	// ParentFilterExpr is the `fk = parent` term both unlink verbs carry: it
	// is what makes a `disconnect` structurally incapable of touching another
	// parent's rows, and on `clear` it is the entire filter (PRD §9.9.6).
	// The `connect` adoption's verify read carries it too, which is how that
	// read counts only the rows now parented here. On M2M it filters the
	// junction's local FK instead, and the junction client is the one that
	// runs the statement.
	ParentFilterExpr string
	// FKUnlinkAssignExpr SETs the traversed FK to NULL through the target's
	// update input. It must be the `omittable.Set[*T](nil)` spelling, never
	// the zero `omittable.Value`: on a nullable FK bound to a pointer
	// type both compile in this position, and the second omits the column from
	// the SET list — matching its rows, reporting success and unlinking
	// nothing. Empty on M2M, where the link is a junction row rather than a
	// column.
	FKUnlinkAssignExpr string
	// DiscFilterField / DiscFilterExpr scope the two unlink verbs to the edge's
	// own discriminator value — the same scoping a nested `create` sets and the
	// `connect` visibility read checks. Without them a `clear` on one
	// polymorphic edge unlinks the rows of every sibling edge over the same
	// foreign key, which is a silent over-unlink rather than a wrong error.
	// Both empty when the edge declares no `discriminator:`.
	DiscFilterField string
	DiscFilterExpr  string
	// ChildCreatePKField is the target's primary-key member on the `create`
	// verb's element type, and ChildCreatePKOmittable reports whether it is
	// wrapped. It exists only so the verb-conflict check can catch a `create`
	// entry carrying an explicit key that `disconnect` also names; a create
	// input that declares no key member (a database-generated one, say) leaves
	// it empty and the check is skipped, which is correct — such an entry
	// always mints a new row and can contradict nothing.
	ChildCreatePKField     string
	ChildCreatePKOmittable bool

	// --- m2m link ---

	// JunctionStructName / JunctionClientField / JunctionConflictTarget name
	// the junction the link rows go through, and JunctionLocalField /
	// JunctionReferenceField its two FK members on the junction's create
	// input. JunctionLocalAssignExpr sets the local one from the parent's key.
	JunctionStructName      string
	JunctionClientField     string
	JunctionConflictTarget  string
	JunctionLocalField      string
	JunctionReferenceField  string
	JunctionLocalAssignExpr string
	// JunctionReferenceAssignExpr sets the junction's reference FK from one
	// target id, spelled `tid`.
	JunctionReferenceAssignExpr string
	// JunctionPKStructName and its two members spell the `<Junction>PK` values
	// an M2M `disconnect` hands HardDeleteMany (PRD §9.9.6). The junction's
	// key has to be exactly the two foreign keys for the pair to be
	// expressible, so a junction with a surrogate key carries no `disconnect`.
	JunctionPKStructName          string
	JunctionPKLocalField          string
	JunctionPKReferenceField      string
	JunctionPKLocalAssignExpr     string
	JunctionPKReferenceAssignExpr string
	// JunctionLocalFilterField is the junction filter member an M2M `clear`
	// filters on; ParentFilterExpr above is the term it takes.
	JunctionLocalFilterField string
}

// TableTenancyContext holds per-table tenancy metadata threaded into the
// TableContext for template consumption. When Tenanted is false, every other
// field is zero and templates MUST emit non-tenancy code. When Tenanted is
// true, the emitted code resolves the tenant via the client's TenantResolver
// and auto-filters queries / mutations on the tenant column (PRD §29.4).
type TableTenancyContext struct {
	// Tenanted is true when this table opts into the tenancy auto-filter.
	Tenanted bool
	// Column is the SQL tenant column name (e.g. "workspace_id").
	Column string
	// FieldName is the PascalCase Go field name for the tenant column
	// (e.g. "WorkspaceID"). Used by generator-side mismatch checks.
	FieldName string
	// GoType is the resolved Go type expression (e.g. "uuid.UUID").
	GoType string
	// Import is the import path for the Go type (empty for builtins).
	Import string
	// Required reflects the resolved tenancy.required setting.
	// True means ErrMissing bubbles up when the resolver returns its zero
	// value (PRD §29.3.1).
	Required bool
	// InPrimaryKey is true when the tenant column participates in the
	// table's DDL primary key. Drives the §29.7 composite-PK
	// constructor-omission rule.
	InPrimaryKey bool
}

// TableContext holds all pre-computed data for rendering table templates.
type TableContext struct {
	// StructName is the Go struct name (singular PascalCase, e.g., "Product").
	StructName string
	// SnakeName is the snake_case form of StructName (e.g., "product") — the
	// stem of every per-table file: `<SnakeName>_gen.go`, the API schema
	// `<SnakeName>_gen.graphqls`, and the gqlgen resolver seed. Derived from
	// the SQL name rather than read back out of StructName, which is not
	// reversible (PRD §8.5).
	SnakeName string
	// TableName is the SQL table name (e.g., "products").
	TableName string
	// TableNameConstant is the generated constant name (e.g., "TableProducts").
	TableNameConstant string
	// Schema is the SQL schema name (e.g., "public").
	Schema string
	// Description is the resolved doc comment for the struct.
	Description string
	// Columns is the sorted list of all columns.
	Columns []ColumnContext
	// PKColumns is the list of primary key columns.
	PKColumns []ColumnContext
	// PKDeclaredInSchema reports whether PKColumns is a PRIMARY KEY the schema
	// declared, so the database carries a primary-key index for it. It is false
	// for a key asserted only through tables.<name>.primary_key.columns (PRD
	// §8.6), even when a UNIQUE over the same columns backs it: that index is
	// the UNIQUE's. The manifest's indexes[] reads it (PRD §30.7).
	PKDeclaredInSchema bool
	// PKStrategy is the primary key generation strategy.
	PKStrategy config.PKStrategy
	// UUIDVersion is the UUID generation version when PKStrategy is "app" (e.g., "v4", "v7").
	UUIDVersion config.UUIDVersion
	// PKAutoGenExpr is the Go expression the create/upsert templates emit for
	// a caller-omitted primary key under PKStrategy "app" — the package's
	// selected UUID integration spelled at UUIDVersion, in the value or
	// string form the PK's Go type needs (PRD §7.4 "Generating UUID values",
	// §8.6). Empty for every other strategy. Filled by attachUUIDGeneration
	// after the whole package's contexts exist, because the integration is a
	// package-wide fact one table cannot see.
	PKAutoGenExpr string
	// CompositePK indicates a multi-column primary key.
	CompositePK bool
	// CompositePKStructName is the Go struct name for the composite PK type.
	CompositePKStructName string
	// SoftDelete holds soft delete column info, or nil if not detected.
	SoftDelete *SoftDeleteContext
	// ExcludeDeleted indicates whether methods should inject default "is not deleted" conditions.
	ExcludeDeleted bool
	// UpdateColumns lists auto-set columns (e.g., "updated_at").
	UpdateColumns []UpdateColumnContext
	// Relationships holds pre-computed relationship data, sorted by name.
	Relationships []RelationshipContext
	// Operations holds the resolved operation flags.
	Operations ResolvedOperations
	// ConflictTargets holds upsert conflict targets, sorted by constant name.
	ConflictTargets []ConflictTargetContext
	// IncrementColumns lists the columns eligible for Increment: arithmetic,
	// and identifying nothing — no PK, no FK, not the tenant column (PRD §8.2).
	// Assembled by buildIncrementColumns and re-derived by
	// attachTenancyToTables, which is where the tenant fact lands.
	IncrementColumns []ColumnContext
	// FilterFields holds the column-to-comparator mapping, sorted by field name.
	FilterFields []FilterFieldContext
	// RelationshipFilters holds one entry per list relationship that
	// contributes a member to this table's filter struct (PRD §11.1).
	RelationshipFilters []RelationshipFilterContext
	// HasTenantedRelationshipFilter reports whether compiling this table's
	// relationship filters can reach a tenanted target — directly, or through
	// a chain of relationship filters. When true the generated read and
	// filter-accepting mutation methods resolve the tenant and pass it down as
	// a FilterOption, so the EXISTS carries the target's tenant predicate.
	HasTenantedRelationshipFilter bool
	// HasFilterOptions mirrors the package-level decision to emit the
	// FilterOption plumbing: true when any entity in the package contributes a
	// relationship filter member, in which case every ToConditions in the
	// package takes `opts ...FilterOption` so recursion and call sites stay
	// uniform. False leaves ToConditions byte-identical to its form without
	// relationship filters.
	HasFilterOptions bool
	// RelationshipOptionsDefs holds unique RelationshipOptions struct definitions
	// for O2M/M2M relationships referenced by this table's FieldOptions.
	RelationshipOptionsDefs []RelationshipOptionsDef
	// RelationshipTargetClients is the per-table deduped list of target struct
	// names for O2M/M2M relationships. Each entry yields a single
	// `<target>Client *<target>Client` field on the entity client, so
	// sub-categorized relationships (PRD §13.7) targeting the same table emit
	// the field once. Unlike RelationshipOptionsDefs, this list is NOT subject
	// to the global cross-table dedup performed by
	// deduplicateRelationshipOptionsDefs.
	RelationshipTargetClients []string
	// NestedWriteClients is the per-table deduped list of struct names whose
	// clients the nested-mutation executors write through and the relationship
	// loaders do not already declare: the junction of every M2M edge (the link
	// rows, PRD §9.9.6) and the target of every has-one edge (the nested
	// `create`). Only generated tables contribute. Each entry yields one
	// `<name>Client *<name>Client` field on the entity client, and the unified
	// client wires the same slice, so the two cannot disagree. Names already in
	// RelationshipTargetClients are excluded, so a junction or has-one target
	// that is also an O2M / M2M target of this table emits one field rather
	// than two. Filled by wireNestedMutationSurface.
	NestedWriteClients []string
	// HasNestedSurface reports whether any (parent, edge) pair on this table
	// can carry a nested-mutation surface (PRD §9.9.5), on the structural
	// facts nestedCandidateEdges reads. It gates the entity client's
	// callbackMode field, which the nested methods pass to the transaction
	// they open. Filled by wireNestedMutationSurface.
	HasNestedSurface bool
	// Nested holds the resolved nested-mutation surface (PRD §9.9), or nil
	// when no edge on this table survives the eligibility rules. Filled
	// by wireNestedMutations, which runs after the tenancy and
	// relationship-filter passes because eligibility reads the *target's*
	// resolved facts. HasNestedSurface above is the structural claim the name
	// registry makes and is deliberately wider than this.
	Nested *NestedContext
	// CreateInputFields holds classified fields for CreateInput.
	CreateInputFields []InputFieldContext
	// UpdateInputFields holds classified fields for UpdateInput.
	UpdateInputFields []InputFieldContext
	// ScanShapes holds per-column scan target information.
	ScanShapes []ScanShapeContext
	// AllColumnNames is the sorted list of all column SQL names,
	// used for the {table}AllColumns variable in generated code.
	AllColumnNames []string
	// VarName is the short variable name for scan targets (e.g., "p" for Product).
	VarName string
	// HasO2ORelationships indicates whether the table has any O2O relationships.
	HasO2ORelationships bool
	// HasO2MRelationships indicates whether the table has any O2M or M2M relationships.
	HasO2MRelationships bool
	// O2ORelationships holds only the O2O relationships, sorted by name.
	O2ORelationships []RelationshipContext
	// O2MRelationships holds only the O2M relationships, sorted by name.
	O2MRelationships []RelationshipContext
	// M2MRelationships holds only the M2M relationships, sorted by name.
	M2MRelationships []RelationshipContext
	// O2OJoinDetails holds the tree of O2O join details for the relationships template.
	// Includes chained O2O joins as nested ChainedJoins.
	O2OJoinDetails []O2OJoinDetail
	// O2OAllTargets is a flat list of all O2O join targets at all depths, sorted by alias.
	// Used by the scan function to generate all scan switch cases.
	O2OAllTargets []O2OJoinDetail
	// O2OAssignOrder is O2OAllTargets in reverse depth order (deepest chain first).
	// Used for NULL detection assignments where children must be assigned before parents.
	O2OAssignOrder []O2OJoinDetail
	// O2OParentScanCases holds parent table scan cases with alias prefix for the JOIN scan function.
	O2OParentScanCases []O2OScanCase
	// Dialect is the SQL dialect name ("postgres", "mysql", "sqlite").
	Dialect config.Dialect
	// Driver is the output driver ("pgx", "stdlib").
	Driver config.Driver
	// BatchSize is the effective batch size for CreateMany.
	BatchSize int
	// PageSize is the effective page size for pagination.
	PageSize int
	// QueryLimit is the effective query limit.
	QueryLimit int
	// CursorKeys is the effective cursor key columns.
	CursorKeys []string
	// StrictUpdates indicates whether updates should fail on missing PK.
	StrictUpdates bool
	// Imports is the deduplicated, sorted list of import paths needed by this table's generated code.
	Imports []string
	// Package is the Go package name for generated files.
	Package string
	// Tenancy carries per-table tenancy metadata. Non-nil with Tenanted=false
	// means the project has tenancy enabled but this table is shared or
	// opted-out; nil means tenancy is globally disabled.
	Tenancy *TableTenancyContext
	// TenantedO2OChildren lists the alias + tenant column of every tenanted
	// O2O target reachable from this table (direct and chained). Used by the
	// o2o branch of GetMany to append child-side tenant filters to the outer
	// WHERE `Conditions` slice (PRD §29.10). Empty when no o2o child is
	// tenanted; the template emits no child-tenant block in that case.
	TenantedO2OChildren []TenantedO2OChild
	// HasTenantedO2OChild is a shortcut for len(TenantedO2OChildren) > 0,
	// used as the template condition and also to decide whether the parent
	// client needs a tenantResolver field / resolveTenant method even when
	// the parent table itself is not tenanted.
	HasTenantedO2OChild bool

	// ExplicitTenantGoType is the element type of CallOptions.Tenant — the
	// package's uniform tenant type (§29.2.4), drawn from any tenanted entity,
	// table or view. Empty when that field is not emitted, which is the same
	// condition shared_types.go.tmpl gates it on: tenancy enabled AND at least
	// one tenanted entity.
	//
	// It is distinct from Tenancy.GoType, which is this *table's* tenant column
	// type and is empty on a shared table that needs no resolver. A
	// relationship loader forwards the explicit tenant to targets that may be
	// tenanted when the parent is not, so it needs the package answer
	// (PRD §29.4.4).
	ExplicitTenantGoType string
}

// TenantedO2OChild describes one tenanted O2O target reachable from a parent
// table. The alias is the JOIN alias assigned by buildO2OJoinDetails; the
// tenant column is read from the child's own per-table tenancy config, never
// inherited from the parent (PRD §29.10).
type TenantedO2OChild struct {
	// Alias is the JOIN alias for the child target (e.g., "c", "co").
	Alias string
	// TenantColumn is the child's tenant SQL column name. Resolved from the
	// child's per-table tenancy config, falling back to the global default.
	TenantColumn string
}

// ViewContext holds all pre-computed data for rendering view templates.
type ViewContext struct {
	// StructName is the Go struct name (singular PascalCase).
	StructName string
	// SnakeName is the snake_case form of StructName — the stem of the
	// generated `<SnakeName>_gen.go`. Derived from the SQL name, not read back
	// out of StructName (PRD §8.5).
	SnakeName string
	// ViewName is the SQL view name.
	ViewName string
	// TableName is an alias for ViewName, used by shared templates
	// (filter, field options, get-input) that reference .TableName.
	TableName string
	// TableNameConstant is the generated constant name (e.g., "TableProductSummaries").
	TableNameConstant string
	// Schema is the SQL schema name.
	Schema string
	// Description is the resolved doc comment for the struct.
	Description string
	// Columns is the sorted list of all columns.
	Columns []ColumnContext
	// PKColumns is the list of PK columns (from @pk annotation), or nil.
	PKColumns []ColumnContext
	// HasPK indicates whether @pk annotation is present.
	HasPK bool
	// CompositePK indicates a multi-column primary key (from @pk with multiple columns).
	CompositePK bool
	// CompositePKStructName is the Go struct name for the composite PK type.
	CompositePKStructName string
	// FilterFields holds the column-to-comparator mapping.
	FilterFields []FilterFieldContext
	// AllColumnNames is the sorted list of all column SQL names.
	AllColumnNames []string
	// VarName is the short variable name for scan targets (e.g., "p" for ProductSummary).
	VarName string
	// ScanShapes holds per-column scan target information.
	ScanShapes []ScanShapeContext
	// Dialect is the SQL dialect name.
	Dialect config.Dialect
	// Driver is the output driver.
	Driver config.Driver
	// PageSize is the effective page size for pagination.
	PageSize int
	// QueryLimit is the effective query limit.
	QueryLimit int
	// CursorKeys is the effective cursor key columns. Empty when HasConnection
	// is false (views with inherited defaults whose columns do not match the
	// view's projection omit Connection per PRD §4.13).
	CursorKeys []string
	// HasConnection reports whether the view should emit the Connection
	// cursor-pagination method. False when the resolved cursor_keys do not
	// exist on the view (views have no PK fallback — see PRD §4.13).
	HasConnection bool
	// Imports is the deduplicated, sorted list of import paths needed by this view's generated code.
	Imports []string
	// Package is the Go package name.
	Package string
	// Materialized reports whether this is a PostgreSQL MATERIALIZED VIEW.
	// When true, the refresh template emits Refresh (and RefreshConcurrently
	// when ConcurrentlyRefreshable); the read surface is unchanged.
	Materialized bool
	// ConcurrentlyRefreshable reports whether RefreshConcurrently is emitted —
	// true when the matview carries a qualifying unique index (introspection)
	// or an @pk annotation asserts one. Always false for regular views.
	ConcurrentlyRefreshable bool
	// Tenancy holds the view's resolved tenancy metadata, or nil when tenancy
	// is globally disabled. Tenanted views scope all five read methods on the
	// tenant column; a view has no write path to scope (PRD §29.2.5). The
	// TableTenancyContext type is shared with tables — its fields are a
	// superset — but InPrimaryKey is never set for a view.
	Tenancy *TableTenancyContext
	// Relationships is always empty for views — present for shared template compatibility.
	Relationships []RelationshipContext
	// RelationshipOptionsDefs is always empty for views — present for shared template compatibility.
	RelationshipOptionsDefs []RelationshipOptionsDef
	// RelationshipFilters is always empty for views — a view has no
	// relationships, so it contributes no EXISTS members. Present for shared
	// template compatibility.
	RelationshipFilters []RelationshipFilterContext
	// HasTenantedRelationshipFilter is always false for views — present for
	// shared template compatibility.
	HasTenantedRelationshipFilter bool
	// HasFilterOptions mirrors the package-level FilterOption decision so a
	// view's ToConditions carries the same signature as a table's.
	HasFilterOptions bool
}

// SharedTypesContext holds pre-computed data for shared generic input types
// generated once per package (not per table). This includes CallOptions,
// PaginateInput, ConnectionInput, IncrementInput, and helper functions
// resolveCallOptions and toAnySlice. PaginateResult is generated by
// pagination.go.tmpl; Connection types by connection.go.tmpl.
type SharedTypesContext struct {
	// Package is the Go package name for generated files.
	Package string
	// Imports is the deduplicated list of import paths.
	Imports []string
	// Types holds the shared generic type definitions, sorted by name.
	Types []SharedTypeDefinition
	// Helpers holds the shared helper function definitions, sorted by name.
	Helpers []SharedHelperDefinition
	// Decls holds whole declaration blocks — doc comment included — that are
	// neither a plain struct type nor a plain function, so they cannot be
	// spelled as a Type or a Helper. Emitted verbatim, in order, after both.
	// Empty for packages that need none, which keeps their shared_types_gen.go
	// byte-identical.
	Decls []string
}

// SharedTypeDefinition holds the definition of a single shared generic type.
type SharedTypeDefinition struct {
	// Name is the Go type name (e.g., "CallOptions").
	Name string
	// TypeParams is the generic type parameter clause (e.g., "[FO any]").
	TypeParams string
	// Doc is the doc comment for the type.
	Doc string
	// Fields holds the struct fields in declaration order.
	Fields []SharedFieldDefinition
}

// SharedFieldDefinition holds one field in a shared generic type.
type SharedFieldDefinition struct {
	// Name is the Go field name (e.g., "SkipCache").
	Name string
	// GoType is the Go type expression (e.g., "bool", "*FO").
	GoType string
	// JSONTag is the snake_case JSON tag (e.g., "skip_cache").
	JSONTag string
	// Doc is the inline comment for the field.
	Doc string
}

// SharedHelperDefinition holds the definition of a shared helper function.
type SharedHelperDefinition struct {
	// Name is the function name (e.g., "resolveCallOptions").
	Name string
	// Signature is the full function signature (e.g., "[FO any](opts []func(*CallOptions[FO])) CallOptions[FO]").
	Signature string
	// Doc is the doc comment for the function.
	Doc string
	// Body is the function body (without enclosing braces).
	Body string
}

// TableNameEntry holds a single table/view entry for TableName constant generation.
type TableNameEntry struct {
	// ConstantName is the whole Go constant name (e.g., "TableProducts"), as
	// TableConstantName spells it. The template emits it verbatim so this and
	// TableContext.TableNameConstant cannot drift.
	ConstantName string
	// SQLName is the SQL table or view name (e.g., "products").
	SQLName string
	// Value is the constant's value: "schema.name" when the entity has a
	// schema (every PostgreSQL table and view), the bare SQL name otherwise
	// (PRD §8.5). The runtime identifies a table by this value alone — the
	// hook helpers compare it and the cache facade switches on it — so two
	// same-named tables in different schemas must not share it.
	Value string
}

// --- Template file contexts ---

// ErrorFileContext holds pre-computed data for rendering the error template.
type ErrorFileContext struct {
	// Package is the Go package name for the generated file.
	Package string
	// Imports is the deduplicated list of import paths.
	Imports []string
}

// TableNameFileContext holds pre-computed data for rendering the tablename template.
type TableNameFileContext struct {
	// Package is the Go package name for the generated file.
	Package string
	// Imports is the deduplicated list of import paths.
	Imports []string
	// Tables is the sorted list of table name entries for TableName constants.
	Tables []TableNameEntry
	// Views is the sorted list of view name entries for TableName constants.
	Views []TableNameEntry
}

// SetContext holds pre-computed data for rendering a single MySQL SET type.
type SetContext struct {
	// Name is the SQL set name (e.g., "permissions_set").
	Name string
	// Schema is the SQL schema name.
	Schema string
	// GoTypeName is the PascalCase Go set type name (e.g., "PermissionSet").
	// This is the named slice type used as the column Go type.
	GoTypeName string
	// ValueGoTypeName is the PascalCase Go value type name (e.g., "PermissionSetValue").
	// This is the string-based enum for individual allowed values.
	ValueGoTypeName string
	// Values is the ordered list of allowed set values.
	Values []string
	// DocComment is the SQL COMMENT value, if any.
	DocComment string
}

// EnumFileContext wraps all enum contexts for a single generated file.
type EnumFileContext struct {
	// Package is the Go package name for the generated file.
	Package string
	// Imports is the deduplicated list of import paths.
	Imports []string
	// Enums is the sorted list of all enum contexts.
	Enums []EnumContext
	// Dialect is the SQL dialect (e.g., "postgres", "mysql", "sqlite").
	// Used to conditionally generate dialect-specific types like enum slices.
	Dialect config.Dialect
}

// SetFileContext wraps all set contexts for a single generated file.
type SetFileContext struct {
	// Package is the Go package name for the generated file.
	Package string
	// Imports is the deduplicated list of import paths.
	Imports []string
	// Sets is the sorted list of all set contexts.
	Sets []SetContext
}

// TypeFileContext wraps all type contexts for a single generated file.
type TypeFileContext struct {
	// Package is the Go package name for the generated file.
	Package string
	// Imports is the deduplicated list of import paths.
	Imports []string
	// Composites holds composite type contexts.
	Composites []CompositeTypeContext
	// Domains holds domain type contexts.
	Domains []DomainTypeContext
	// Extras holds extra type contexts.
	Extras []ExtraTypeContext
}

// O2OJoinDetail holds pre-computed data for one O2O LEFT JOIN relationship.
// Used by the relationships template to generate resolveO2OJoins and
// scanWithOneToOneJoins functions.
type O2OJoinDetail struct {
	// FieldOptionsCheck is the Go condition to check if this join is requested.
	// E.g., "fo.Company != nil" for direct, nested checks for chained.
	FieldOptionsCheck string
	// FieldOptionsColumns is the Go expression to get selected columns.
	// E.g., "fo.Company.Columns()"
	FieldOptionsColumns string

	// TargetTable is the SQL table name.
	TargetTable string
	// TargetSchema is the SQL schema name.
	TargetSchema string
	// Alias is the JOIN alias (e.g., "c", "co").
	Alias string

	// ON condition parts
	// OnLocalAlias is the alias of the parent / chaining table (e.g., "p" or "c" for chained).
	OnLocalAlias string
	// OnLocal is the parent-side column: the FK on the parent, or the
	// parent's first PK when FKOnTarget (has-one).
	OnLocal string
	// OnRemote is the target-side column: the target's first PK, or the FK on
	// the target when FKOnTarget (has-one).
	OnRemote string
	// Filter is the polymorphic filter expression (empty if none).
	Filter string

	// Soft delete on target
	// HasSoftDelete indicates the target table has soft delete.
	HasSoftDelete bool
	// SoftDeleteColumn is the soft delete column name on the target.
	SoftDeleteColumn string
	// SoftDeleteTypeConst is the Go constant expression (e.g., "sql.SoftDeleteTimestamp").
	SoftDeleteTypeConst string

	// Scan function data
	// StructName is the Go struct name of the target entity.
	StructName string
	// VarName is the variable name for the scan target (e.g., "c").
	VarName string
	// HasDataVar is the bool variable name for NULL detection (e.g., "cHasData").
	HasDataVar string
	// PKFieldName is the PK Go field name (e.g., "ID").
	PKFieldName string
	// PKColumn is the target's PK SQL column name (e.g., "id"). The join's
	// column list unions it in so NULL detection — which tests PKFieldName
	// against PKZeroValue — always has a scanned value to test.
	PKColumn string
	// PKZeroValue is the Go zero value for the PK type (e.g., "uuid.Nil").
	PKZeroValue string
	// ParentVarName is the variable to assign the relationship to (e.g., "p").
	ParentVarName string
	// FieldName is the Go field on the parent struct (e.g., "Company").
	FieldName string
	// ScanCases holds the scan switch cases for this target's columns.
	ScanCases []O2OScanCase
	// ChainedJoins holds O2O relationships that chain through this one.
	ChainedJoins []O2OJoinDetail
}

// O2OScanCase holds one scan switch case in the JOIN-aware scan function.
type O2OScanCase struct {
	// PrefixedColumn is the alias-prefixed column name (e.g., "c.id").
	PrefixedColumn string
	// ScanExpr is the scan target expression (e.g., "&c.ID").
	ScanExpr string
	// IsPK marks this as the PK column (triggers hasData flag).
	IsPK bool
	// Shape is the scan shape: "direct", "wrapped".
	Shape string
	// FieldName is the Go field name (for post-scan assignment target).
	FieldName string
	// VarName is the entity variable name (for post-scan assignment).
	VarName string
}

// SorterContext holds all pre-computed data for rendering the sorter template.
// Generates per-table type-safe sorter structs with methods returning sql.Sort.
type SorterContext struct {
	// Package is the Go package name for generated files.
	Package string
	// Imports is the deduplicated list of import paths.
	Imports []string
	// Tables is the sorted list of tables with their sortable columns.
	Tables []SorterTableContext
}

// SorterTableContext holds sort data for a single table.
type SorterTableContext struct {
	// StructName is the Go struct name (e.g., "Product").
	StructName string
	// SorterTypeName is the sorter struct name (e.g., "ProductSorter").
	SorterTypeName string
	// Columns holds all columns eligible for sorting.
	Columns []SorterColumnContext
}

// SorterColumnContext holds data for one sortable column.
type SorterColumnContext struct {
	// ColumnName is the SQL column name (e.g., "created_at").
	ColumnName string
	// MethodName is the PascalCase method name (e.g., "CreatedAt").
	MethodName string
}

// PaginationContext holds pre-computed data for rendering the pagination template.
// Generates the PaginateResult[T] generic type.
type PaginationContext struct {
	// Package is the Go package name for generated files.
	Package string
	// Imports is the deduplicated list of import paths.
	Imports []string
}

// ConnectionContext holds pre-computed data for rendering the connection template.
// Generates Connection[T], Edge[T], PageInfo, and cursor helper functions.
type ConnectionContext struct {
	// Package is the Go package name for generated files.
	Package string
	// Imports is the deduplicated list of import paths.
	Imports []string
}

// ClientWire represents a cross-client reference that needs to be wired up
// after construction (e.g., categoryClient.productClient = c.product).
type ClientWire struct {
	// EntityField is the field name on the entity client (e.g., "productClient").
	EntityField string
	// TargetField is the field name on the unified Client (e.g., "product").
	TargetField string
}

// EntityClientContext holds minimal data for one entity in the unified client.
type EntityClientContext struct {
	// StructName is the Go struct name (e.g., "Product", "ProductSummary").
	StructName string
	// InterfaceName is the exported interface name (e.g., "ProductClient").
	InterfaceName string
	// FieldName is the unexported field name on the unified Client struct.
	FieldName string
	// AccessorName is the exported method name on the Client struct
	// (e.g., "Products" for tables, "ProductSummary" for views).
	AccessorName string
	// ClientWires holds cross-client references needed for O2M/M2M relationship loading.
	// Each wire maps a field on this entity's client struct to a target entity's client field on the unified Client.
	ClientWires []ClientWire
	// IsView indicates whether this is a view (read-only) client.
	IsView bool
}

// ClientContext holds pre-computed data for the unified client template.
type ClientContext struct {
	// Entities lists all entity clients (tables + views), sorted by struct name.
	Entities []EntityClientContext
	// ClientName is the exported struct name (default "Client", configurable via output.client.name).
	ClientName string
	// Package is the Go package name.
	Package string
	// Imports is the sorted list of import paths needed by the unified client.
	Imports []string
	// EventsEnabled indicates whether the event system is enabled globally.
	// When true, the client template adds event publisher fields and wiring.
	EventsEnabled bool
	// CacheEnabled indicates whether caching is enabled globally.
	// When true, the client template adds a Cache option field and WithCache shorthand.
	CacheEnabled bool
	// CacheConfig holds the resolved cache render context (populated when CacheEnabled).
	CacheConfig CacheRenderCtx
	// CachedTables lists tables with caching enabled (populated when CacheEnabled).
	CachedTables []CachedTable
	// CachedViews lists views with caching enabled (populated when CacheEnabled).
	CachedViews []CachedView
	// ViewInvalidateMap maps a source table's hook.TableName value to the list of view
	// BuildTablePattern strings to invalidate (populated when CacheEnabled).
	ViewInvalidateMap map[string][]string
	// TenancyEnabled indicates whether the project has tenancy globally
	// enabled. When true, the client template emits a tenantResolver field
	// and a WithTenantResolver ClientOption (PRD §29.3).
	TenancyEnabled bool
	// TenancyGoType is the concrete Go tenant type expression used in the
	// TenantResolver[T] signature (e.g. "uuid.UUID"). Populated only when
	// TenancyEnabled.
	TenancyGoType string
	// TenancyImport is the import path for TenancyGoType (empty for
	// builtins). Populated only when TenancyEnabled.
	TenancyImport string
	// TenantedEntities is the sorted list of per-entity-client field names
	// (matching EntityClientContext.FieldName) that should receive the
	// tenantResolver during client construction. Populated only when
	// TenancyEnabled.
	TenantedEntities []string
	// NestedSurfaceEntities is the sorted list of per-entity-client field names
	// (matching EntityClientContext.FieldName) that carry a callbackMode field
	// because they can grow a nested-mutation surface (PRD §9.9). The unified
	// client assigns it after construction, as it does the tenant resolver —
	// the entity-client constructor takes the same argument list for every
	// table, and the mode is a client-wide option rather than a per-table one.
	NestedSurfaceEntities []string
	// Dialect is the project dialect (e.g. "postgres", "mysql", "sqlite").
	// The unified-client template branches on this for MySQL-only emission of
	// the mysqlVersionCache type and its plumbing into entity clients
	// (PRD §9.6a — version-aware NoWait/SkipLocked guard).
	Dialect string
}

// HasTenantedNestedEdge reports that at least one nested-mutation edge writes
// through a client that resolves a tenant — the edge's target, or on an M2M the
// junction the link step writes (NestedEdgeContext.TargetResolvesTenant).
//
// It is what makes the nested method hoist one resolve onto the transaction ctx
// so every inner write inherits it, satisfying PRD §29.6's once-per-mutation
// bound that §29.10 extends to nested children. Without the hoist each inner
// call resolves again.
//
// Deliberately a fact about the edges rather than about this table: a shared
// parent with a tenanted child is a legitimate §29.10 shape and needs the
// resolver exactly as a tenanted parent does.
func (t TableContext) HasTenantedNestedEdge() bool {
	if t.Nested == nil {
		return false
	}
	return slices.ContainsFunc(t.Nested.Edges, func(e NestedEdgeContext) bool {
		return e.TargetResolvesTenant
	})
}

// NeedsTenantResolver is the union that decides whether this client carries a
// tenantResolver field and the resolveTenant / tenantValue helpers: the table is
// itself tenanted (PRD §29.3.3), it appends a tenanted O2O child's filter at
// GetMany time (§29.10), one of its relationship filters reaches a tenanted
// target so the EXISTS subquery needs that target's predicate (§11.1 invariant
// 1), or it hoists a nested mutation's single resolve (§29.6).
//
// A method rather than a pre-computed field, against the general rule in
// guidelines/TEMPLATES.md §6, because it is read from three places — the emitted
// field, the emitted helpers, and applyClientTenancy's assignment list — and the
// three must agree or the package does not compile. A field would need every
// hand-built test fixture to set it — a wiring rule a fixture can silently
// skip, leaving the field at its zero value; the inputs it reads are all
// plain facts a fixture already sets. Value receiver so templates can call it on
// a non-addressable context.
func (t TableContext) NeedsTenantResolver() bool {
	return t.resolvesTenantDirectly() || t.HasTenantedNestedEdge()
}

// resolvesTenantDirectly is NeedsTenantResolver minus the nested-edge term: the
// three reasons a client resolves a tenant for a call on its *own* surface.
//
// It exists to break the recursion the nested term would otherwise introduce. A
// nested edge asks whether its target resolves, and the target may itself carry
// nested edges — so answering with NeedsTenantResolver would make the result
// depend on the order wireNestedMutations happens to visit tables in. One level
// is also all that is correct: v1 nests one level deep (PRD §9.9, `max_depth`),
// so a parent's nested write invokes its targets' direct surfaces and never
// their nested methods.
//
// The three terms it reads are all settled before wireNestedMutations runs —
// attachTenancyToTables for the first two, wireRelationshipFilters for the third
// — which is what makes it safe to read off a target mid-pass.
func (t TableContext) resolvesTenantDirectly() bool {
	return (t.Tenancy != nil && t.Tenancy.Tenanted) ||
		t.HasTenantedO2OChild || t.HasTenantedRelationshipFilter
}
