// Package manifest builds the sqlgen-emitted manifest document — a structured,
// versioned description of the generated package consumed by AI agents, the
// MCP server (PRD §31), and Go callers via the runtime-embedded
// Client.Manifest() / ManifestEntity() / ManifestVersion() methods (PRD §30.6).
//
// The types defined here mirror the canonical JSON shape per PRD §30.4 so
// EmitJSON can marshal a Document directly into the on-disk file without an
// intermediate transformation step.
//
// Build is read-only against the parser schema, the resolved config, and the
// in-memory TableContext / ViewContext / APIContext slices already produced
// by the codegen pipeline — no re-parsing.
package manifest

// SchemaVersion is the JSON-Schema-versioned contract for the emitted manifest
// document. Held at 0.1.0: the 1.0.0 freeze is deferred until the version
// bump is greenlit, paired with the schema/v1.json conventions tightening
// (PRD §30.5).
const SchemaVersion = "0.1.0"

// Document is the top-level manifest envelope serialized into manifest_gen.json
// (single layout) or as the index file (per-entity layout). See PRD §30.4.
type Document struct {
	Schema           string           `json:"$schema,omitempty"`
	SchemaVersion    string           `json:"schema_version"`
	GeneratedAt      string           `json:"generated_at"`
	Generator        Generator        `json:"generator"`
	Dialect          string           `json:"dialect"`
	Package          string           `json:"package"`
	Layout           string           `json:"layout"`
	Conventions      Conventions      `json:"conventions"`
	GenerationConfig GenerationConfig `json:"generation_config"`
	Entities         []Entity         `json:"entities"`
	Enums            []Enum           `json:"enums"`
	Extras           []Extra          `json:"extras"`
}

// Generator identifies the tool that emitted the manifest.
type Generator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// generatorName is the value Build stamps into Generator.Name. The stale-cleanup
// pass reads it back off disk to decide whether a manifest directory is
// sqlgen's before removing it (see sqlgenOwnsManifestDir), so emission and
// ownership must not be able to drift apart — they share this constant.
const generatorName = "sqlgen"

// GenerationConfig is the resolved, secret-stripped feature-toggle snapshot
// for the generated package — pure booleans, no DSNs or file paths.
// See PRD §30.4.3.
type GenerationConfig struct {
	AuditColumns  bool `json:"audit_columns"`
	Cache         bool `json:"cache"`
	Events        bool `json:"events"`
	GraphQL       bool `json:"graphql"`
	GraphTopLevel bool `json:"graph_top_level"`
	SoftDelete    bool `json:"soft_delete"`
	Tenancy       bool `json:"tenancy"`
	Views         bool `json:"views"`
}

// Conventions captures the package-wide reference surface answered once and
// cross-referenced from per-entity entries. See PRD §30.4.1.
type Conventions struct {
	ClientEntryPoints       ClientEntryPoints     `json:"client_entry_points"`
	ErrorSentinels          []ErrorSentinel       `json:"error_sentinels"`
	ConstraintErrorCodes    map[string]string     `json:"constraint_error_codes"`
	FindReturnsNilOnMissing bool                  `json:"find_returns_nil_on_missing"`
	Pagination              PaginationConvention  `json:"pagination"`
	CallOptions             CallOptionsConvention `json:"call_options"`
	SoftDelete              SoftDeleteConvention  `json:"soft_delete"`
	Comparator              ComparatorConvention  `json:"comparator"`
	Omittable               OmittableConvention   `json:"omittable"`
}

// clientAccessorPattern is how generated code reaches an entity client:
// one accessor on the unified Client per entity, carrying reads and writes
// alike (PRD §20). Tables use the plural struct name (client.Users()), views
// the singular one (client.ActiveUser()).
const clientAccessorPattern = "client.<Entity>()"

// ClientEntryPoints describes how an entity client is reached. The query and
// mutation keys are separate because the manifest schema types them so; the
// generated client has no such split and both hold the same pattern.
type ClientEntryPoints struct {
	Query    string `json:"query"`
	Mutation string `json:"mutation"`
}

// ErrorSentinel describes one of the package-wide error sentinel values.
//
// Name is the bare Go identifier; Package qualifies it — the generated
// package for the sentinels re-exported by PRD §22.1, "tenancy" for the two
// runtime tenancy sentinels. GraphQLCode carries the PRD §26.5.5 mapping and
// is empty for sentinels with no 1:1 GraphQL code, including
// ErrConstraintViolation, which maps 1:N by ConstraintError.Type — see
// Conventions.ConstraintErrorCodes.
type ErrorSentinel struct {
	Name        string `json:"name"`
	GraphQLCode string `json:"graphql_code"`
	Package     string `json:"package"`
}

// PaginationConvention describes the pagination shape.
type PaginationConvention struct {
	PageType           string `json:"page_type"`
	ListEnvelopeSuffix string `json:"list_envelope_suffix"`
	CursorEncoding     string `json:"cursor_encoding"`
}

// CallOptionsConvention describes the CallOptions surface.
type CallOptionsConvention struct {
	Type   string   `json:"type"`
	Fields []string `json:"fields"`
}

// SoftDeleteConvention describes the soft-delete inclusion semantics.
type SoftDeleteConvention struct {
	DefaultExcludedFromFinds bool   `json:"default_excluded_from_finds"`
	IncludeVia               string `json:"include_via"`
	HardDeleteMethodSuffix   string `json:"hard_delete_method_suffix"`
	RestoreMethodSuffix      string `json:"restore_method_suffix"`
}

// ComparatorConvention describes the comparator package's surface.
type ComparatorConvention struct {
	Package     string            `json:"package"`
	Families    []string          `json:"families"`
	ShapeNotes  []string          `json:"shape_notes"`
	Composition map[string]string `json:"composition"`
	Examples    map[string]string `json:"examples,omitempty"`
}

// OmittableConvention describes the omittable package's surface.
type OmittableConvention struct {
	Package      string   `json:"package"`
	Type         string   `json:"type"`
	Purpose      string   `json:"purpose"`
	Construction []string `json:"construction"`
	Methods      []string `json:"methods"`
	JSONBehavior string   `json:"json_behavior"`
}

// Entity is a single generated entity — table or view. See PRD §30.4.2.
type Entity struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Table  string `json:"table"`
	Schema string `json:"schema,omitempty"`
	Source string `json:"source,omitempty"`
	// Materialized marks a PostgreSQL materialized view (kind == "view" only).
	// Advertises the refresh capability: a materialized entity's mutation
	// methods include Refresh (and RefreshConcurrently when a qualifying
	// unique index or @pk annotation establishes it). Omitted for tables and
	// regular views.
	Materialized  bool           `json:"materialized,omitempty"`
	FilePrefix    string         `json:"file_prefix"`
	Files         []string       `json:"files"`
	Comment       string         `json:"comment"`
	Indexes       []Index        `json:"indexes"`
	PK            PK             `json:"pk"`
	Features      Features       `json:"features"`
	Columns       []Column       `json:"columns"`
	Relationships []Relationship `json:"relationships"`
	Methods       Methods        `json:"methods"`
	Filter        Filter         `json:"filter"`
	Sort          Sort           `json:"sort"`
	Examples      *Examples      `json:"examples,omitempty"`
}

// PK describes the primary-key shape.
type PK struct {
	Kind    string     `json:"kind"` // "single" | "composite" | "none"
	Struct  string     `json:"struct,omitempty"`
	Columns []PKColumn `json:"columns,omitempty"`
}

// PKColumn names one of the primary-key columns.
type PKColumn struct {
	Name    string `json:"name"`
	GoField string `json:"go_field"`
	GoType  string `json:"go_type"`
}

// Features bundles every per-entity feature-flag block. Nil-valued blocks are
// omitted from the JSON so consumers can rely on the presence of a block as
// the activation signal.
type Features struct {
	SoftDelete   *SoftDeleteFeature `json:"soft_delete"`
	Cache        *CacheFeature      `json:"cache"`
	Events       *EventsFeature     `json:"events"`
	Tenancy      *TenancyFeature    `json:"tenancy"`
	AuditColumns []string           `json:"audit_columns,omitempty"`
}

// SoftDeleteFeature carries the soft-delete column + strategy when configured.
type SoftDeleteFeature struct {
	Column string `json:"column"`
	Type   string `json:"type"` // "timestamp" | "bool" | "integer"
}

// CacheFeature carries the cache configuration for the entity.
type CacheFeature struct {
	TTLSeconds    int      `json:"ttl_seconds"`
	Hydration     string   `json:"hydration"`
	KeyPattern    string   `json:"key_pattern"`
	InvalidatesOn []string `json:"invalidates_on"`
}

// EventsFeature carries the event-emission configuration for the entity.
type EventsFeature struct {
	Enabled      bool     `json:"enabled"`
	Types        []string `json:"types"`
	PayloadShape string   `json:"payload_shape"`
}

// TenancyFeature carries the tenancy configuration for the entity.
//
// MismatchError is omitempty because a read-only entity omits it entirely: a
// view has no mutation input to mismatch against, so advertising an error it
// can never return would be worse than omitting the key (PRD §30.4.2). Tables
// always populate the field. This struct is hand-duplicated between the
// builder-side cmd/sqlgen/manifest package and this stdlib-only runtime one;
// TestFeatureStructs_noDriftBetweenBuilderAndRuntime pins them field-for-field
// including tag options.
type TenancyFeature struct {
	Column               string `json:"column"`
	Mode                 string `json:"mode"`
	MissingResolverError string `json:"missing_resolver_error"`
	MismatchError        string `json:"mismatch_error,omitempty"`
}

// Index describes a single declared index on the entity.
type Index struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique"`
	Method  string   `json:"method"`
	Where   string   `json:"where,omitempty"`
}

// Column describes a single column on an entity, including the PRD §30.7
// extended metadata fields (comment, check, default_kind).
type Column struct {
	Name        string `json:"name"`
	GoField     string `json:"go_field"`
	GoType      string `json:"go_type"`
	DBType      string `json:"db_type"`
	Nullable    bool   `json:"nullable"`
	PK          bool   `json:"pk"`
	Unique      bool   `json:"unique"`
	Default     string `json:"default,omitempty"`
	DefaultKind string `json:"default_kind,omitempty"`
	Auto        string `json:"auto,omitempty"`
	Comparator  string `json:"comparator,omitempty"`
	Comment     string `json:"comment"`
	Check       string `json:"check,omitempty"`
	// Access is the column's access role (PRD §32.2): "read_only",
	// "write_only", "hidden", or "internal". Omitted for public (the
	// default). Additive against schema 0.1.0 — older manifests without the
	// field decode as public.
	Access string `json:"access,omitempty"`
	// Redacted marks columns whose values must not be surfaced by manifest
	// consumers (true for write_only / internal roles). Method sql_bodies
	// stay accurate — this marker is what tells MCP / agents not to
	// advertise the value or suggest exposing the column (PRD §32.3).
	Redacted bool `json:"redacted,omitempty"`
}

// Relationship describes one generated relationship edge.
type Relationship struct {
	Name         string    `json:"name"`
	Kind         string    `json:"kind"` // "o2o" | "o2m" | "m2o" | "m2m"
	TargetEntity string    `json:"target_entity"`
	FK           *FK       `json:"fk,omitempty"`
	Junction     *Junction `json:"junction,omitempty"`
	Filter       string    `json:"filter,omitempty"`
}

// FK names the foreign-key table+column for O2O / O2M / M2O relationships.
// Table is the table that holds Column: the target for a has-one or has-many
// edge, the entity's own table for a belongs-to edge.
//
// Table is the bare name and Schema its schema, split the way Entity splits
// them (PRD §30.4.2) and omitted when empty, so same-named tables in two
// schemas stay distinguishable.
type FK struct {
	Table  string `json:"table"`
	Schema string `json:"schema,omitempty"`
	Column string `json:"column"`
}

// Junction names the M2M junction table and its two FK columns.
//
// Table is the bare name and Schema its schema, split the way Entity splits
// them (PRD §30.4.2) and omitted when empty, so same-named junctions in two
// schemas stay distinguishable.
type Junction struct {
	Table    string `json:"table"`
	Schema   string `json:"schema,omitempty"`
	LocalFK  string `json:"local_fk"`
	TargetFK string `json:"target_fk"`
}

// Methods groups generated query and mutation methods on an entity.
type Methods struct {
	Query    []Method `json:"query"`
	Mutation []Method `json:"mutation"`
}

// Method describes one generated client method.
type Method struct {
	Name      string            `json:"name"`
	Params    []MethodParam     `json:"params"`
	Returns   string            `json:"returns"`
	Errors    []string          `json:"errors"`
	Notes     string            `json:"notes,omitempty"`
	Source    *MethodSource     `json:"source,omitempty"`
	SQLBodies map[string]string `json:"sql_bodies,omitempty"`
}

// MethodParam describes one parameter of a generated method.
type MethodParam struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// MethodSource notes the discovery origin of a method that is not generated
// for every entity. Nothing populates it today — the unique-index-derived
// FindBy* methods it was added for are generated nowhere and no longer
// advertised. The field and its schema entry are retained because
// both are optional and removing them would churn the contract for nothing.
type MethodSource struct {
	Kind    string   `json:"kind"`
	Columns []string `json:"columns,omitempty"`
}

// Filter describes the generated <Entity>Filter type.
type Filter struct {
	Type   string        `json:"type"`
	Fields []FilterField `json:"fields"`
}

// FilterField names one column-to-comparator binding on a filter.
type FilterField struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Sort describes the generated <Entity>Sort type.
type Sort struct {
	Type   string   `json:"type"`
	Fields []string `json:"fields"`
}

// Examples carries optional read/write code samples per entity. Populated only
// when manifest.include_examples is true (default).
type Examples struct {
	Read  []string `json:"read,omitempty"`
	Write []string `json:"write,omitempty"`
}

// Enum describes a generated enum type for the manifest's enums[] section.
type Enum struct {
	Name        string   `json:"name"`
	GoType      string   `json:"go_type"`
	DBType      string   `json:"db_type"`
	Values      []string `json:"values"`
	GraphQLName string   `json:"graphql_name,omitempty"`
	SliceType   string   `json:"slice_type,omitempty"`
}

// Extra describes a generated composite, domain alias, or extra type for the
// manifest's extras[] section.
type Extra struct {
	Kind     string       `json:"kind"` // "composite" | "domain" | "extra"
	Name     string       `json:"name"`
	GoType   string       `json:"go_type"`
	BaseType string       `json:"base_type,omitempty"`
	Fields   []ExtraField `json:"fields,omitempty"`
	Source   string       `json:"source,omitempty"`
	JSONOnly bool         `json:"json_only,omitempty"`
}

// ExtraField names one field on a composite type or extra struct.
type ExtraField struct {
	Name   string `json:"name"`
	GoType string `json:"go_type"`
	JSON   string `json:"json,omitempty"`
}
