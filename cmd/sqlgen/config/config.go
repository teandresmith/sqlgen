// Package config handles YAML configuration loading, validation, and defaults
// for the sqlgen code generator.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Dialect identifies the SQL dialect used for parsing.
type Dialect string

// Supported SQL dialects.
const (
	DialectPostgres Dialect = "postgres"
	DialectMySQL    Dialect = "mysql"
	DialectSQLite   Dialect = "sqlite"
)

// Source identifies the schema source mode.
type Source string

// Supported schema source modes.
const (
	SourceFiles    Source = "files"
	SourceDatabase Source = "database"
	SourceBoth     Source = "both"
)

// ParseMode controls how duplicate table definitions across files are handled.
type ParseMode string

// Supported parse modes.
const (
	ParseModeStrict ParseMode = "strict"
	ParseModeMerge  ParseMode = "merge"
)

// Driver identifies the Go database driver imported by generated code.
type Driver string

// Supported output drivers.
const (
	DriverPgx    Driver = "pgx"
	DriverStdlib Driver = "stdlib"
)

// Layout identifies the file layout strategy for generated table code.
type Layout string

// Supported output layouts.
const (
	LayoutSingleFile   Layout = "single_file"
	LayoutFilePerTable Layout = "file_per_table"
)

// UUIDVersion identifies the UUID generation version for app-generated PKs.
type UUIDVersion string

// Supported UUID generation versions.
const (
	UUIDVersionV4 UUIDVersion = "v4"
	UUIDVersionV7 UUIDVersion = "v7"
)

// PKStrategy identifies the primary key generation strategy.
type PKStrategy string

// Supported primary key generation strategies.
const (
	PKStrategyDB     PKStrategy = "db"
	PKStrategyApp    PKStrategy = "app"
	PKStrategyCaller PKStrategy = "caller"
)

// SoftDeleteType identifies the SQL value category used by a soft delete column.
type SoftDeleteType string

// Supported soft delete type categories.
const (
	SoftDeleteTimestamp SoftDeleteType = "timestamp"
	SoftDeleteBool      SoftDeleteType = "bool"
	SoftDeleteInteger   SoftDeleteType = "integer"
)

// Serializer identifies the cache value serialization format.
type Serializer string

// Supported cache serializers.
const (
	SerializerJSON    Serializer = "json"
	SerializerMsgpack Serializer = "msgpack"
	SerializerCustom  Serializer = "custom"
)

// JSONLayout selects between the manifest's single-file and per-entity JSON
// emission strategies. See PRD §30.2 / §30.4.
type JSONLayout string

// Supported manifest JSON layouts.
const (
	JSONLayoutSingle    JSONLayout = "single"
	JSONLayoutPerEntity JSONLayout = "per_entity"
)

// Manifest format identifiers (closed set per PRD §30.2).
const (
	ManifestFormatJSON     = "json"
	ManifestFormatMarkdown = "markdown"
)

// IsValid reports whether d is one of the defined Dialect constants.
func (d Dialect) IsValid() bool {
	switch d {
	case DialectPostgres, DialectMySQL, DialectSQLite:
		return true
	}
	return false
}

// IsValid reports whether s is one of the defined Source constants.
func (s Source) IsValid() bool {
	switch s {
	case SourceFiles, SourceDatabase, SourceBoth:
		return true
	}
	return false
}

// IsValid reports whether p is one of the defined ParseMode constants.
func (p ParseMode) IsValid() bool {
	switch p {
	case ParseModeStrict, ParseModeMerge:
		return true
	}
	return false
}

// IsValid reports whether d is one of the defined Driver constants.
func (d Driver) IsValid() bool {
	switch d {
	case DriverPgx, DriverStdlib:
		return true
	}
	return false
}

// IsValid reports whether l is one of the defined Layout constants.
func (l Layout) IsValid() bool {
	switch l {
	case LayoutSingleFile, LayoutFilePerTable:
		return true
	}
	return false
}

// IsValid reports whether v is one of the defined UUIDVersion constants.
func (v UUIDVersion) IsValid() bool {
	switch v {
	case UUIDVersionV4, UUIDVersionV7:
		return true
	}
	return false
}

// IsValid reports whether s is one of the defined PKStrategy constants.
func (s PKStrategy) IsValid() bool {
	switch s {
	case PKStrategyDB, PKStrategyApp, PKStrategyCaller:
		return true
	}
	return false
}

// IsValid reports whether t is one of the defined SoftDeleteType constants.
func (t SoftDeleteType) IsValid() bool {
	switch t {
	case SoftDeleteTimestamp, SoftDeleteBool, SoftDeleteInteger:
		return true
	}
	return false
}

// IsValid reports whether s is one of the defined Serializer constants.
func (s Serializer) IsValid() bool {
	switch s {
	case SerializerJSON, SerializerMsgpack, SerializerCustom:
		return true
	}
	return false
}

// IsValid reports whether l is one of the defined JSONLayout constants.
func (l JSONLayout) IsValid() bool {
	switch l {
	case JSONLayoutSingle, JSONLayoutPerEntity:
		return true
	}
	return false
}

// RootConfig is the top-level configuration for sqlgen.
type RootConfig struct {
	Version    string           `yaml:"version"`
	Input      InputConfig      `yaml:"input"`
	Output     OutputConfig     `yaml:"output"`
	Generation GenerationConfig `yaml:"generation"`
	Overrides  OverrideConfig   `yaml:"overrides"`
	Events     *EventConfig     `yaml:"events"`
	Cache      *CacheConfig     `yaml:"cache"`
	Tenancy    *TenancyConfig   `yaml:"tenancy"`
	API        *APIConfig       `yaml:"api"`
	// ExcludeTables filters tables out of generation regardless of input
	// source (file or introspection). Patterns use glob syntax: `*` matches
	// any sequence, `?` matches any single character; case-sensitive. A
	// table matched by any pattern is dropped before context build.
	// See PRD §6.4.
	ExcludeTables []string               `yaml:"exclude_tables"`
	Tables        map[string]TableConfig `yaml:"tables"`
	Views         map[string]ViewConfig  `yaml:"views"`
	Enums         map[string]EnumConfig  `yaml:"enums"`
	Extras        map[string]ExtraType   `yaml:"extras"`

	// CacheKeyPrefixDefaulted is true when applyDefaults filled in
	// cache.key_prefix = "sqlgen" because the parsed YAML left it empty
	// (either unset or explicit ""). ValidatePreParse reads this flag
	// to emit the defaulted-key_prefix stderr warning.
	CacheKeyPrefixDefaulted bool `yaml:"-"`
}

// InputConfig controls where the schema is read from.
type InputConfig struct {
	Dialect    Dialect           `yaml:"dialect"`
	Source     Source            `yaml:"source"`
	Schema     string            `yaml:"schema"`
	Paths      []string          `yaml:"paths"`
	Views      []string          `yaml:"views"`
	ParseMode  ParseMode         `yaml:"parse_mode"`
	Connection *ConnectionConfig `yaml:"connection"`
	Introspect *IntrospectConfig `yaml:"introspect"`
}

// ConnectionConfig holds database connection parameters.
type ConnectionConfig struct {
	URL      string `yaml:"url"`
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"` //nolint:gosec // config field, not a hardcoded credential
	Database string `yaml:"database"`
	SSLMode  string `yaml:"ssl_mode"`
}

// IntrospectConfig controls schema/table filtering for database introspection.
// To exclude tables, use the top-level `exclude_tables` field on RootConfig
// (PRD §6.4) — it applies to introspected and file-based inputs alike.
type IntrospectConfig struct {
	Schemas        []string `yaml:"schemas"`
	ExcludeSchemas []string `yaml:"exclude_schemas"`
	Tables         []string `yaml:"tables"`
}

// OutputConfig controls where generated code is written.
type OutputConfig struct {
	Driver  Driver              `yaml:"driver"`
	Dir     string              `yaml:"dir"`
	Package string              `yaml:"package"`
	Layout  Layout              `yaml:"layout"`
	Enums   *EnumOutputConfig   `yaml:"enums"`
	Types   *TypeOutputConfig   `yaml:"types"`
	Client  *ClientOutputConfig `yaml:"client"`

	// dirDefaulted records that Dir was unset and applyOutputDefaults filled in
	// "." (PRD §4.2). Defaulting is destructive of that distinction — an
	// explicit `dir: "."` and an omitted `dir:` are indistinguishable
	// afterwards — and only the omitted form is worth warning about, so the
	// signal is captured at the moment it is still available. Unexported, so
	// YAML neither reads nor writes it and a directly-constructed config
	// (tests) leaves it false.
	dirDefaulted bool
}

// EnumOutputConfig overrides the output location for enums.
type EnumOutputConfig struct {
	File    string `yaml:"file"`
	Package string `yaml:"package"`
}

// EnumConfig customizes one schema enum's generated identity (PRD §4.11).
// Keyed in RootConfig.Enums by SQL enum name, bare or schema-qualified, the
// same resolution TableConfig uses.
//
// StructName renames ONE identity, not two: the GraphQL enum name IS the Go
// type name (there is no separate GraphQL alias), so the override moves the Go
// type, its constants, the All<Type> var, the Validate<Type> function, the
// named slice type, the gqlgen models: binding, the monomorphized comparator
// input and its translator, and the manifest's go_type together. Its primary
// use is resolving a GraphQL type-name collision, which is a generation error
// (PRD §26.4 "GraphQL type name ownership") — the enum whose PascalCased name
// lands on a name sqlgen already owns has no other remedy short of an
// `ALTER TYPE … RENAME` migration.
type EnumConfig struct {
	// StructName overrides the generated Go type name. Empty means the
	// PascalCased SQL enum name, as StructName derives it.
	StructName string `yaml:"struct_name"`
	// Description is the Go doc comment on the generated type. It takes
	// precedence over the SQL `COMMENT ON TYPE` when both are present.
	Description string `yaml:"description"`
}

// TypeOutputConfig overrides the output location for composite/domain types.
type TypeOutputConfig struct {
	File    string `yaml:"file"`
	Package string `yaml:"package"`
}

// ClientOutputConfig controls unified client output settings.
type ClientOutputConfig struct {
	Name string `yaml:"name"`
	File string `yaml:"file"`
}

// GenerationConfig holds global defaults that control code generation and runtime behavior.
type GenerationConfig struct {
	QueryLimit        *int               `yaml:"query_limit"`
	BatchSize         *int               `yaml:"batch_size"`
	PageSize          *int               `yaml:"page_size"`
	CursorKeys        []string           `yaml:"cursor_keys"`
	UUIDVersion       UUIDVersion        `yaml:"uuid_version"`
	StrictUpdates     *bool              `yaml:"strict_updates"`
	SoftDeleteColumns []SoftDeleteConfig `yaml:"soft_delete_columns"`
	UpdateColumns     []string           `yaml:"update_columns"`
	ExcludeColumns    []string           `yaml:"exclude_columns"`
	ExcludeDeleted    *bool              `yaml:"exclude_deleted"`
	Manifest          *ManifestConfig    `yaml:"manifest"`

	// RemovedOperations exists only to catch a leftover
	// `generation.operations` key. The Go client generates every method the
	// schema allows and has no per-operation switch (PRD §4.6), but deleting
	// the field outright would let the strict decoder reject the key with a
	// bare unknown-field error that never says where the control went.
	// Decoding it here lets validation name `api.operations` instead
	// (PRD §4.13). Nothing else reads it.
	RemovedOperations yaml.Node `yaml:"operations"`

	// NestedMutations is the package-wide opt-in for the three …WithRelated
	// methods (PRD §4.6 / §9.9). A nil block, or one whose Enabled is unset
	// or false, emits nothing nested anywhere in the package.
	NestedMutations *NestedMutationsConfig `yaml:"nested_mutations"`
}

// NestedMutationsConfig is the package-wide nested-mutation block (PRD §4.6).
// Opt-in and off by default: with Enabled unset or false no nested method,
// wrapper input type, or nested GraphQL mutation is emitted anywhere. It is the
// only client-side control over the nested methods (PRD §4.6).
//
// Operations and Verbs are closed sets — nestedMutationOperations and
// nestedMutationVerbs — and an unset slice means the full default set rather
// than the empty one, so omitting the key is not a silent narrowing.
// MaxDepth is a pointer because 1 is both the default and the only accepted
// value in v1 (PRD §4.13): a plain int would make an omitted key read as 0,
// which validation rejects.
type NestedMutationsConfig struct {
	Enabled    *bool    `yaml:"enabled"`
	Operations []string `yaml:"operations"`
	Verbs      []string `yaml:"verbs"`
	MaxDepth   *int     `yaml:"max_depth"`
}

// TableNestedMutationsConfig narrows which relationships a nested mutation on
// this table may reach (PRD §4.8). It has no effect unless
// generation.nested_mutations.enabled is true.
//
// An omitted Relationships list includes every eligible edge — the eligibility
// rules are conservative and the feature is globally opt-in, so an
// allowlist-always default would silently give a newly added table no nested
// surface. When the list is present, only the named edges are reachable and a
// named edge that fails an eligibility rule is a hard error rather than a
// silent omission (PRD §9.9).
type TableNestedMutationsConfig struct {
	Relationships []TableNestedRelationship `yaml:"relationships"`
}

// TableNestedRelationship is one entry in a table's nested-mutation edge
// allowlist (PRD §4.8).
//
// AllowReparent lets an O2M `connect` on this edge adopt a row that is already
// parented elsewhere; with the default, such a row is ErrAlreadyRelated. It is
// per-edge config and never a per-call option — moving a row between parents is
// a static, reviewable policy decision, and a CallOptions flag would make it a
// per-request authorization decision, which PRD §29.11 puts out of scope.
type TableNestedRelationship struct {
	Name          string `yaml:"name"`
	AllowReparent bool   `yaml:"allow_reparent"`
}

// nestedMutationOperations is the closed set accepted in
// generation.nested_mutations.operations (PRD §4.6). Also the default when the
// key is omitted.
var nestedMutationOperations = []string{"create", "update", "upsert"}

// nestedMutationVerbs is the closed set accepted in
// generation.nested_mutations.verbs (PRD §4.6). Also the default when the key
// is omitted.
var nestedMutationVerbs = []string{"create", "connect", "disconnect", "clear"}

// NestedMutationsMaxDepth is the only nesting depth v1 implements (PRD §4.6 /
// §9.9). The config field exists so a future depth is a config change rather
// than a breaking default, and validation rejects every other value so a
// future depth cannot silently mean depth 1.
const NestedMutationsMaxDepth = 1

// ManifestConfig configures the optional AI-friendly manifest emission.
// See PRD §30.2. When the YAML omits the `manifest:` block entirely, the
// pointer is nil and no manifest artifacts are produced.
//
// All boolean fields are pointers so an unset value can be distinguished
// from an explicit false. applyManifestDefaults fills the unset fields when
// Enabled resolves to true.
type ManifestConfig struct {
	Enabled          *bool             `yaml:"enabled"`
	Formats          []string          `yaml:"formats"`
	JSONFilename     string            `yaml:"json_filename"`
	JSONLayout       JSONLayout        `yaml:"json_layout"`
	JSONPerEntityDir string            `yaml:"json_per_entity_dir"`
	MarkdownDir      string            `yaml:"markdown_dir"`
	IncludeExamples  *bool             `yaml:"include_examples"`
	IncludeInternal  *bool             `yaml:"include_internal"`
	Breadcrumbs      BreadcrumbsConfig `yaml:"breadcrumbs"`
	EmbedInClient    *bool             `yaml:"embed_in_client"`
	MCP              *MCPConfig        `yaml:"mcp"`
}

// MCPConfig configures the project-local MCP registration files sqlgen emits
// alongside the manifest (MCP.md §3.4). The block is global-only: it nests
// under generation.manifest and has no per-table analogue (TableManifestConfig
// stays Enabled-only per PRD §30.2), so resolution is global → default.
//
// EmitProjectConfig is a pointer so an unset value can be distinguished from
// an explicit false; applyManifestDefaults fills it (and the other fields)
// with the §3.4 defaults when manifest.enabled resolves true.
type MCPConfig struct {
	EmitProjectConfig *bool    `yaml:"emit_project_config"`
	ProjectConfigs    []string `yaml:"project_configs"`
	ServerKey         string   `yaml:"server_key"`
	CommandTemplate   string   `yaml:"command_template"`
}

// BreadcrumbsConfig controls the agent-discoverable breadcrumb files emitted
// alongside the manifest. Each sub-flag defaults to true when the parent
// manifest block is enabled. See PRD §30.2 / §30.3.
type BreadcrumbsConfig struct {
	ClaudeMD   *bool `yaml:"claude_md"`
	AgentsMD   *bool `yaml:"agents_md"`
	PackageDoc *bool `yaml:"package_doc"`
}

// TableManifestConfig is the per-table manifest override. Only Enabled is
// supported, and only as an opt-out (false) — a per-table true with a
// global false is rejected at validation. See PRD §30.2.
type TableManifestConfig struct {
	Enabled *bool `yaml:"enabled"`
}

// SoftDeleteConfig identifies a column name and type used for soft delete detection.
type SoftDeleteConfig struct {
	Name string         `yaml:"name"`
	Type SoftDeleteType `yaml:"type"`
}

// Operations is an `api.operations` mask (PRD §26.5.1), in its dual
// string/struct form: either a preset string (e.g., "read_only") or explicit
// keys layered on an optional preset. The Go client has no such block — it
// generates every method the schema allows (PRD §4.6).
type Operations struct {
	Preset      string `yaml:"preset"`
	Get         *bool  `yaml:"get"`
	GetMany     *bool  `yaml:"get_many"`
	Create      *bool  `yaml:"create"`
	CreateMany  *bool  `yaml:"create_many"`
	Update      *bool  `yaml:"update"`
	UpdateMany  *bool  `yaml:"update_many"`
	UpdateWhere *bool  `yaml:"update_where"`
	Upsert      *bool  `yaml:"upsert"`
	UpsertMany  *bool  `yaml:"upsert_many"`
	SoftDelete  *bool  `yaml:"soft_delete"`
	Restore     *bool  `yaml:"restore"`
	HardDelete  *bool  `yaml:"hard_delete"`
	Exists      *bool  `yaml:"exists"`
	Count       *bool  `yaml:"count"`
	Increment   *bool  `yaml:"increment"`
	Paginate    *bool  `yaml:"paginate"`
	Connection  *bool  `yaml:"connection"`
	Stream      *bool  `yaml:"stream"`

	// The three nested-mutation methods (PRD §4.6 / §9.9). Each requires its
	// base operation — validateNestedWithRelatedBase rejects an explicit true
	// beside a resolved-false base, in an `api.operations` mask as well, where
	// the mask conjoins each toggle with its base (§26.5.1) — and each emits
	// nothing unless generation.nested_mutations.enabled is true and the table
	// has an eligible relationship.
	CreateWithRelated *bool `yaml:"create_with_related"`
	UpdateWithRelated *bool `yaml:"update_with_related"`
	UpsertWithRelated *bool `yaml:"upsert_with_related"`
}

// UnmarshalYAML handles the dual string/struct form of operations.
// A bare string like "read_only" is treated as a preset.
func (o *Operations) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		o.Preset = value.Value
		return nil
	}

	// For mapping nodes, decode into a temporary struct to avoid infinite recursion.
	type operationsAlias Operations
	var alias operationsAlias
	if err := value.Decode(&alias); err != nil {
		return fmt.Errorf("decoding operations: %w", err)
	}
	*o = Operations(alias)
	return nil
}

// OverrideConfig holds global SQL-to-Go type mapping overrides.
type OverrideConfig struct {
	UsePointers *bool                   `yaml:"use_pointers"`
	Types       map[string]TypeOverride `yaml:"types"`
}

// TypeOverride configures a custom Go type mapping for a SQL type.
//
// The named type must be able to cross the driver boundary on its own: it
// implements [database/sql.Scanner] and [database/sql/driver.Valuer], or the
// driver special-cases it (uuid.UUID, which database/sql handles in both
// directions and pgx routes through UUIDCodec). A type that does neither is
// wrapped in one of your own that does, and the override names the wrapper.
// See PRD §4.7.
type TypeOverride struct {
	Type      string          `yaml:"type"`
	Import    string          `yaml:"import"`
	ZeroValue string          `yaml:"zero_value"`
	Nullable  NullableVariant `yaml:"nullable"`
}

// NullableVariant wraps the dual string/struct form for nullable type overrides.
// Shorthand: "uuid.NullUUID" (same import as parent).
// Full form: {type, import, underlying_field, valid_field, valid_method, valid_invert}.
//
// UnderlyingField names the embedded scalar field on the wrapper struct that
// holds the underlying value (e.g. "UUID" for uuid.NullUUID, "Decimal" for
// decimal.NullDecimal). Set this for wrappers participating in FK
// stringification; built-in integrations populate it automatically.
//
// ValidField, ValidMethod, and ValidInvert describe how to query the wrapper's
// validity. They are mutually exclusive — set at most one of ValidField /
// ValidMethod, defaulting to ValidField "Valid" (the database/sql.NullX
// convention). ValidInvert negates a ValidMethod result for wrappers that
// signal *invalidity* (e.g. time.Time.IsZero()).
type NullableVariant struct {
	Type            string `yaml:"type"`
	Import          string `yaml:"import"`
	UnderlyingField string `yaml:"underlying_field"`
	ValidField      string `yaml:"valid_field"`
	ValidMethod     string `yaml:"valid_method"`
	ValidInvert     bool   `yaml:"valid_invert"`
}

// UnmarshalYAML handles the dual string/struct form for nullable overrides.
func (n *NullableVariant) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		n.Type = value.Value
		return nil
	}

	type nullableAlias NullableVariant
	var alias nullableAlias
	if err := value.Decode(&alias); err != nil {
		return fmt.Errorf("decoding nullable variant: %w", err)
	}
	*n = NullableVariant(alias)
	return nil
}

// TableConfig customizes a specific table. Key is the table name (bare or schema-qualified).
type TableConfig struct {
	// Table-specific fields.
	StructName           string                    `yaml:"struct_name"`
	Description          string                    `yaml:"description"`
	Overrides            *OverrideConfig           `yaml:"overrides"`
	PrimaryKey           *TablePrimaryKeyConfig    `yaml:"primary_key"`
	ExcludeRelationships []string                  `yaml:"exclude_relationships"`
	Relationships        []TableRelationship       `yaml:"relationships"`
	ColumnMap            map[string]ColumnOverride `yaml:"column_map"`
	TypeMap              map[string]string         `yaml:"type_map"`

	// Event overrides.
	Events *TableEventConfig `yaml:"events"`

	// Cache overrides (tri-state pointers — nil = inherit global).
	Cache *TableCacheConfig `yaml:"cache"`

	// Tenancy overrides (tri-state pointers — nil = inherit global).
	Tenancy *TableTenancyConfig `yaml:"tenancy"`

	// API overrides (tri-state pointers — nil = inherit global).
	API *TableAPIConfig `yaml:"api"`

	// Manifest overrides (per-table opt-out only; tri-state pointer).
	Manifest *TableManifestConfig `yaml:"manifest"`

	// Nested-mutation edge allowlist and per-edge options (PRD §4.8).
	// Inert unless generation.nested_mutations.enabled is true.
	NestedMutations *TableNestedMutationsConfig `yaml:"nested_mutations"`

	// Generation overrides (table-level → global → built-in default).
	QueryLimit     *int     `yaml:"query_limit"`
	BatchSize      *int     `yaml:"batch_size"`
	PageSize       *int     `yaml:"page_size"`
	CursorKeys     []string `yaml:"cursor_keys"`
	ExcludeColumns []string `yaml:"exclude_columns"`
	StrictUpdates  *bool    `yaml:"strict_updates"`

	// RemovedOperations catches a leftover `tables.<name>.operations` key;
	// see GenerationConfig.RemovedOperations.
	RemovedOperations yaml.Node `yaml:"operations"`
}

// TablePrimaryKeyConfig overrides the PK for a specific table.
//
// Strategy and UUIDVersion override the auto-detected INSERT-time PK handling
// (see PRD §8.6). Columns explicitly declares which schema columns sqlgen
// should treat as the table's primary key — used when the SQL schema has no
// PRIMARY KEY clause (uniqueness declared only via ALTER TABLE ADD UNIQUE,
// CREATE UNIQUE INDEX, or enforced at the application level). When Columns
// is non-empty it silently overrides any auto-detected PK; downstream code
// paths (cache keys, event PKs, Get/Update/Exists signatures) read only the
// resolved PK column set.
type TablePrimaryKeyConfig struct {
	Strategy    PKStrategy  `yaml:"strategy"`
	UUIDVersion UUIDVersion `yaml:"uuid_version"`
	Columns     []string    `yaml:"columns"`
}

// TableRelationship defines an explicit relationship on a table.
//
// Side identifies which end of the edge this entry represents — "parent" for
// the FK-holder (default; matches every auto-detected edge), or "child" for
// the referenced side. Manifest output uses this to distinguish o2o from m2o
// for entries that share Type = OneToOne. Omitted / empty defaults to
// "parent" — config-defined relationships almost always describe the
// FK-holding side.
type TableRelationship struct {
	Name                string             `yaml:"name"`
	Type                string             `yaml:"type"`
	Side                string             `yaml:"side"`
	Table               string             `yaml:"table"`
	FK                  string             `yaml:"fk"`
	Junction            string             `yaml:"junction"`
	JunctionLocalFK     string             `yaml:"junction_local_fk"`
	JunctionReferenceFK string             `yaml:"junction_reference_fk"`
	Filter              string             `yaml:"filter"`
	Sort                []RelationshipSort `yaml:"sort"`
	Description         string             `yaml:"description"`

	// Discriminator is the structured form of a polymorphic predicate
	// (PRD §13.4.1). Mutually exclusive with Filter, and the only polymorphic
	// form that is write-eligible for nested mutations — `filter:` is raw SQL
	// and therefore not invertible into something a writer could set.
	Discriminator *RelationshipDiscriminator `yaml:"discriminator"`
}

// RelationshipDiscriminator is the {column, value} form of a polymorphic edge
// predicate (PRD §13.4.1). It compiles to exactly the equality predicate the
// equivalent `filter:` produced, in the same position, so the two forms select
// the same rows on every dialect.
//
// Column names a column on the *related* table; validation checks it exists
// there. Value is a scalar — the PRD types it that way rather than as a
// string, because a discriminator column need not be textual.
//
// The value is bound as a query parameter on the O2M / M2M loader and the
// relationship-filter subquery, and *interpolated* in the O2O JOIN `ON` clause,
// which carries no parameter channel (`sql.JoinClause.On` is opaque SQL by
// contract). Only the O2O form is byte-identical to the `filter:` it replaces;
// on the bound paths the emitted Go differs because the value moves out of the
// SQL text. See PRD §13.4.1 "Binding".
type RelationshipDiscriminator struct {
	Column string `yaml:"column"`
	Value  any    `yaml:"value"`
}

// RelationshipSort defines a sort order for a relationship loader.
type RelationshipSort struct {
	Column    string `yaml:"column"`
	Direction string `yaml:"direction"`
}

// ColumnOverride overrides individual column properties.
type ColumnOverride struct {
	// Name overrides the Go field name derived from the SQL column name
	// (PRD §8.5 "Field Name Resolution"). It must be a valid *exported* Go
	// identifier; validation rejects anything else.
	//
	// The override renames the Go identifier and nothing a client can
	// observe: the `db` / `json` struct tags, the SQL column constants, the
	// GraphQL field name and the sort-enum value all stay derived from the
	// SQL column name. It is the documented escape hatch for the cases where
	// acronym detection produces an undesirable spelling, and for the
	// digit-leading `Col<N>…` guard.
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Type        string `yaml:"type"`
	Import      string `yaml:"import"`
	// Access is the column's access role (PRD §32.2): one of "public"
	// (default when empty), "read_only", "write_only", "hidden", or
	// "internal". It constrains which external surfaces the column's value
	// escapes through — the generated API, event payloads, and the manifest.
	// It never affects the core Go client: the entity struct, Create/Update
	// inputs, filter, sort, and cache always carry every surviving column.
	Access string `yaml:"access"`
}

// Access roles accepted by tables.<t>.column_map.<col>.access (PRD §32.2).
const (
	AccessPublic    = "public"
	AccessReadOnly  = "read_only"
	AccessWriteOnly = "write_only"
	AccessHidden    = "hidden"
	AccessInternal  = "internal"
)

// IsValidAccessRole reports whether s is a recognized access role.
// The empty string is not a role — it means "unset" and resolves to public.
func IsValidAccessRole(s string) bool {
	switch s {
	case AccessPublic, AccessReadOnly, AccessWriteOnly, AccessHidden, AccessInternal:
		return true
	}
	return false
}

// EventConfig controls global event publishing.
type EventConfig struct {
	Enabled bool `yaml:"enabled"`
}

// TableEventConfig controls per-table event publishing overrides.
// Enabled is a pointer for tri-state semantics: nil (inherit global), true, false.
type TableEventConfig struct {
	Enabled *bool `yaml:"enabled"`
}

// ViewConfig defines a read-only view.
type ViewConfig struct {
	StructName   string           `yaml:"struct_name"`
	SQL          string           `yaml:"sql"`
	Cache        *ViewCacheConfig `yaml:"cache"`
	InvalidateOn []string         `yaml:"invalidate_on"`
	// CursorKeys overrides generation.cursor_keys for this view. When set
	// explicitly, every listed column must exist on the view (hard error).
	// When omitted, the global value is applied if all its columns exist on
	// the view; otherwise the view's Connection method is omitted with a
	// warning. Views have no primary key, so there is no PK fallback.
	// See PRD §4.9 / §4.13.
	CursorKeys []string `yaml:"cursor_keys"`
	// Tenancy overrides tenancy behavior for this view. Same shape and
	// precedence as tables.<name>.tenancy — a view carrying the effective
	// tenant column is scoped by detection, so this block is only needed to
	// opt out or to name a non-standard column. Read path only: a view has
	// no mutations to scope. See PRD §4.9 / §29.2.5.
	Tenancy *TableTenancyConfig `yaml:"tenancy"`
	// API overrides this view's exposure on the generated API surface.
	// See PRD §4.9 / §26.4 "Views on the GraphQL surface".
	API *ViewAPIConfig `yaml:"api"`
}

// ViewAPIConfig overrides per-view API exposure. It is the read-only twin of
// TableAPIConfig: same two knobs, but the operations mask can only ever name
// the three read keys, because a view has no mutation surface to narrow
// (PRD §4.9 / §26.4 "Views on the GraphQL surface").
type ViewAPIConfig struct {
	// Enabled is tri-state: nil (inherit global api.enabled), true (expose),
	// false (exclude this view from the generated API surface entirely — type,
	// queries, inputs, translators and bindings alike). Opt-out only: an
	// explicit true under a global `api.enabled: false` is a config error,
	// matching the tables.<name>.manifest precedent (PRD §4.13).
	Enabled *bool `yaml:"enabled"`
	// Operations masks which of the view's three read keys (get, paginate,
	// connection) the API
	// exposes, replacing the global `api.operations` mask when set.
	// Subtractive only, exactly as TableAPIConfig.Operations is.
	//
	// Naming any operation outside the read set — a mutation, or one of the
	// client-only operations — is a hard error rather than a silent no-op:
	// the block cannot express a mutation on a read-only entity, and saying
	// so beats ignoring a knob the user deliberately turned (PRD §4.13,
	// same spirit as clientOnlyOperationFields).
	Operations *Operations `yaml:"operations"`
}

// CacheConfig controls global caching behavior.
type CacheConfig struct {
	Enabled        bool                  `yaml:"enabled"`
	Version        int                   `yaml:"version"`
	TTL            string                `yaml:"ttl"`
	Serializer     Serializer            `yaml:"serializer"`
	KeyPrefix      string                `yaml:"key_prefix"`
	Hydration      *HydrationConfig      `yaml:"hydration"`
	CircuitBreaker *CircuitBreakerConfig `yaml:"circuit_breaker"`
}

// HydrationConfig controls async cache hydration.
type HydrationConfig struct {
	Enabled bool   `yaml:"enabled"`
	Timeout string `yaml:"timeout"`
}

// CircuitBreakerConfig controls the cache circuit breaker.
type CircuitBreakerConfig struct {
	Enabled           bool   `yaml:"enabled"`
	FailureThreshold  int    `yaml:"failure_threshold"`
	ProbeInterval     string `yaml:"probe_interval"`
	HalfOpenMaxProbes int    `yaml:"half_open_max_probes"`
}

// TableAPIConfig overrides per-table API exposure. Enabled is a tri-state
// pointer: nil (inherit global), true (expose), false (exclude this table
// from the generated API surface). See PRD §26.10.
type TableAPIConfig struct {
	Enabled *bool `yaml:"enabled"`
	// Operations masks which operations this table exposes through the API,
	// replacing the global `api.operations` mask when set. Subtractive only
	// — see APIConfig.Operations. This is the knob for "keep the Go client
	// method but do not expose the mutation", which lets a consumer
	// keep a hand-written resolver as the only door to a table while the
	// blessed resolver still calls the generated client underneath.
	Operations *Operations `yaml:"operations"`
}

// apiOperationField pairs an API-exposable operation's YAML name with an
// accessor into Operations. The generated API surface reads exactly these
// fourteen (PRD §26.5.1), and every key names the client method its surface
// calls: `<table>List` is backed by Paginate and gated on `paginate`, and
// `update<Table>s(filter, input)` is backed by UpdateWhere and gated on
// `update_where`. The remaining operations — get_many, exists, count,
// increment, stream, update_many, upsert_many — name client methods no API
// surface calls.
//
// One table drives masking AND validation so the two can never disagree
// about which operations the API can express.
type apiOperationField struct {
	Name  string
	Field func(*Operations) **bool
}

var apiOperationFields = []apiOperationField{
	{"get", func(o *Operations) **bool { return &o.Get }},
	{"paginate", func(o *Operations) **bool { return &o.Paginate }},
	{"connection", func(o *Operations) **bool { return &o.Connection }},
	{"create", func(o *Operations) **bool { return &o.Create }},
	{"create_many", func(o *Operations) **bool { return &o.CreateMany }},
	{"update", func(o *Operations) **bool { return &o.Update }},
	{"update_where", func(o *Operations) **bool { return &o.UpdateWhere }},
	{"upsert", func(o *Operations) **bool { return &o.Upsert }},
	{"soft_delete", func(o *Operations) **bool { return &o.SoftDelete }},
	{"hard_delete", func(o *Operations) **bool { return &o.HardDelete }},
	{"restore", func(o *Operations) **bool { return &o.Restore }},
	{"create_with_related", func(o *Operations) **bool { return &o.CreateWithRelated }},
	{"update_with_related", func(o *Operations) **bool { return &o.UpdateWithRelated }},
	{"upsert_with_related", func(o *Operations) **bool { return &o.UpsertWithRelated }},
}

// clientOnlyOperationFields are the operations with no API projection.
// Setting one explicitly inside an `api.operations` block is a config error:
// silently ignoring a knob the user deliberately turned is worse than saying
// the block cannot express it.
var clientOnlyOperationFields = []apiOperationField{
	// No list-of-ids query is generated: `<table>List` takes a filter and is
	// backed by Paginate, so it is gated on `paginate`.
	{"get_many", func(o *Operations) **bool { return &o.GetMany }},
	{"exists", func(o *Operations) **bool { return &o.Exists }},
	{"count", func(o *Operations) **bool { return &o.Count }},
	{"increment", func(o *Operations) **bool { return &o.Increment }},
	{"stream", func(o *Operations) **bool { return &o.Stream }},
	// No batch-of-items update mutation is generated: `update<Table>s` takes a
	// filter and is backed by UpdateWhere, so it is gated on `update_where`.
	{"update_many", func(o *Operations) **bool { return &o.UpdateMany }},
	// No batch-upsert mutation is generated: the batch form exists for
	// server-side composition, and the M2M `connect` verb that needs it
	// reaches it through PRD §9.9 rather than a root field (§26.5.1
	// exclusion table, §26.10 closed list).
	{"upsert_many", func(o *Operations) **bool { return &o.UpsertMany }},
}

// viewAPIOperationNames is the read set a view's API surface can express
// (PRD §26.4 "Views on the GraphQL surface"): the three read keys, and
// nothing else. `get_many` is not among them: no API surface calls GetMany,
// so it is a client-only key on views and tables alike. A view has no mutation half — §16.4 makes it read-only — so
// every other operation is unspellable in a `views.<name>.api.operations`
// block rather than merely ineffective.
var viewAPIOperationNames = map[string]bool{
	"get": true, "paginate": true, "connection": true,
}

// mutationAPIOperationFields are the API-exposable operations a view can never
// offer. Derived from apiOperationFields rather than written out a second time,
// so a new API operation lands in exactly one table and is automatically
// rejected on views until someone decides otherwise.
var mutationAPIOperationFields = func() []apiOperationField {
	out := make([]apiOperationField, 0, len(apiOperationFields))
	for _, f := range apiOperationFields {
		if !viewAPIOperationNames[f.Name] {
			out = append(out, f)
		}
	}
	return out
}()

// ResolveViewAPIEnabled reports whether a view is exposed on the generated API
// surface. Resolution order: per-view `api.enabled` → global `api.enabled` →
// false (no API block means no API).
//
// The per-view flag is opt-OUT only; validateViewAPIOptIn rejects an explicit
// true under a global opt-out, so the true branch here can never widen the
// surface past what `api.enabled` already opened (PRD §4.13).
func ResolveViewAPIEnabled(view ViewConfig, api *APIConfig) bool {
	if view.API != nil && view.API.Enabled != nil {
		return *view.API.Enabled
	}
	return api != nil && api.Enabled
}

// ViewAPIOperationsMask returns the operations mask that applies to a view —
// the view's own `api.operations` when set, otherwise the global
// `api.operations`, otherwise nil for "no mask". The same precedence
// APIOperationsMask gives a table, so one global mask covers both entity kinds.
func ViewAPIOperationsMask(view ViewConfig, api *APIConfig) *Operations {
	if view.API != nil && view.API.Operations != nil {
		return view.API.Operations
	}
	if api != nil && api.Operations != nil {
		return api.Operations
	}
	return nil
}

// ResolveViewAPIOperationsMask returns the expanded operations mask that
// applies to a view, or nil when no mask is configured. Preset expansion
// happens here, exactly as in ResolveAPIOperationsMask.
//
// The caller intersects the result with the view's read-only base set, which
// is what keeps the mask subtractive: a mask naming a mutation cannot add one
// to a view, and validateViewAPIOperations rejects the attempt outright.
func ResolveViewAPIOperationsMask(view ViewConfig, api *APIConfig) (*Operations, error) {
	mask := ViewAPIOperationsMask(view, api)
	if mask == nil {
		return nil, nil
	}
	resolved, err := ResolveOperations(*mask)
	if err != nil {
		return nil, err
	}
	return &resolved, nil
}

// APIOperationsMask returns the operations mask that applies to a table —
// the table's own `api.operations` when set, otherwise the global
// `api.operations`, otherwise nil for "no mask".
func APIOperationsMask(table TableConfig, api *APIConfig) *Operations {
	if table.API != nil && table.API.Operations != nil {
		return table.API.Operations
	}
	if api != nil && api.Operations != nil {
		return api.Operations
	}
	return nil
}

// ResolveAPIOperationsMask returns the expanded operations mask that applies
// to a table, or nil when no mask is configured. Preset expansion happens
// here, so an unset field in a mask block reads as the preset's value (a
// bare `{create: false}` expands from the `all` preset and therefore
// subtracts nothing else).
//
// The caller intersects this with the table's own resolved operations, which
// is what makes the mask subtractive: a GraphQL resolver delegates to the Go
// client, so an operation the client does not generate can never be exposed.
func ResolveAPIOperationsMask(table TableConfig, api *APIConfig) (*Operations, error) {
	mask := APIOperationsMask(table, api)
	if mask == nil {
		return nil, nil
	}
	resolved, err := ResolveOperations(*mask)
	if err != nil {
		return nil, err
	}
	return &resolved, nil
}

// TableCacheConfig overrides cache behavior for a specific table.
// All fields are pointers for tri-state semantics: nil means inherit from global.
// Serializer is intentionally not per-table — see PRD §27.10 (single decode
// path per process keeps cache inspection tooling readable; users needing
// per-type encoding implement cache.Serializer as a dispatching wrapper).
type TableCacheConfig struct {
	Enabled *bool   `yaml:"enabled"`
	TTL     *string `yaml:"ttl"`
}

// ViewCacheConfig overrides cache behavior for a specific view.
// View caching is opt-in only (does not inherit global cache.enabled).
type ViewCacheConfig struct {
	Enabled *bool   `yaml:"enabled"`
	TTL     *string `yaml:"ttl"`
}

// ExtraType defines a custom Go struct (e.g., for JSONB columns).
type ExtraType struct {
	Description string                    `yaml:"description"`
	Fields      map[string]ExtraTypeField `yaml:"fields"`
}

// APIConfig controls API generation. Only GraphQL is generated; REST and
// gRPC fields exist for forward compatibility but no code is generated for
// them yet (PRD §26.2 / §26.8 / §26.13).
type APIConfig struct {
	Enabled bool `yaml:"enabled"`
	// Operations masks which operations the generated API surface exposes,
	// across every table. It is the only per-operation control sqlgen has: the
	// Go client generates every method the schema allows (PRD §4.6). It can
	// only *subtract*: a GraphQL mutation needs the Go client method behind
	// it, so an operation the schema does not give the client can never be
	// exposed by the API.
	// Per-table `tables.<name>.api.operations` replaces this wholesale.
	// Absent means no mask — the API exposes whatever the client generates.
	// Surface-agnostic by design: REST and gRPC inherit the same mask when
	// they land. See PRD §26.5.1.
	Operations *Operations       `yaml:"operations"`
	GraphQL    *GraphQLAPIConfig `yaml:"graphql"`
	REST       *RESTAPIConfig    `yaml:"rest"`
	GRPC       *GRPCAPIConfig    `yaml:"grpc"`
}

// GraphQLAPIConfig configures GraphQL schema and resolver generation.
type GraphQLAPIConfig struct {
	Enabled       bool                     `yaml:"enabled"`
	SchemaDir     string                   `yaml:"schema_dir"`
	ResolverDir   string                   `yaml:"resolver_dir"`
	Package       string                   `yaml:"package"`
	MaxDepth      int                      `yaml:"max_depth"`
	MaxComplexity int                      `yaml:"max_complexity"`
	FieldCasing   string                   `yaml:"field_casing"`
	GqlgenConfig  string                   `yaml:"gqlgen_config"`
	GqlgenBin     string                   `yaml:"gqlgen_bin"`
	Scalars       map[string]ScalarBinding `yaml:"scalars"`
}

// ScalarBinding declares a custom GraphQL scalar's Go type and marshaling
// strategy. Marshaling values: "builtin" | "method" | "external".
//
// GoType is spelled as a full import path plus type name
// ("example.com/types.Email", "net/netip.Addr"), matching gqlgen's own model
// paths. The import path and the type name are separated at the LAST dot,
// which is also how gqlgen reads a model path — so a package path that itself
// contains dots ("gopkg.in/yaml.v3.Node") splits correctly.
type ScalarBinding struct {
	GoType     string `yaml:"go_type"`
	Marshaling string `yaml:"marshaling"`
	// MarshalerPackage is the import path of the package declaring the
	// consumer-supplied `Marshal<Scalar>` / `Unmarshal<Scalar>` free
	// functions. Required when Marshaling is "external", rejected otherwise.
	//
	// It is a separate field rather than being derived from GoType's package
	// because the two differ for exactly the cases external marshaling
	// exists to serve: nothing can be added to `net/netip`, nor to any
	// third-party package the consumer does not own.
	//
	// gqlgen infers the bound Go type from the marshaler signature, so the
	// emitted `models:` entry is the discovery anchor
	// `<MarshalerPackage>.<ScalarName>` and needs no type alias
	// (PRD §26.4.1 "Generated file: graph/scalars_gen.go").
	MarshalerPackage string `yaml:"marshaler_package"`
}

// RESTAPIConfig is the REST API config block. Generation is deferred to a
// post-Phase-16 follow-on; the type exists so YAML files can opt-in early.
type RESTAPIConfig struct {
	Enabled  bool               `yaml:"enabled"`
	BasePath string             `yaml:"base_path"`
	Spec     *RESTAPISpecConfig `yaml:"spec"`
}

// RESTAPISpecConfig holds the OpenAPI spec output destination.
type RESTAPISpecConfig struct {
	File string `yaml:"file"`
}

// GRPCAPIConfig is the gRPC API config block. Reserved for a future phase.
type GRPCAPIConfig struct {
	Enabled    bool   `yaml:"enabled"`
	ProtoDir   string `yaml:"proto_dir"`
	ServiceDir string `yaml:"service_dir"`
}

// Field-casing values accepted by api.graphql.field_casing.
const (
	FieldCasingCamel = "camel_case"
	FieldCasingSnake = "snake_case"
)

// Marshaling modes accepted by api.graphql.scalars[*].marshaling.
const (
	ScalarMarshalingBuiltin  = "builtin"
	ScalarMarshalingMethod   = "method"
	ScalarMarshalingExternal = "external"
)

// IsValidFieldCasing reports whether s is a recognized field_casing value.
func IsValidFieldCasing(s string) bool {
	return s == FieldCasingCamel || s == FieldCasingSnake
}

// IsValidScalarMarshaling reports whether m is a recognized marshaling mode.
func IsValidScalarMarshaling(m string) bool {
	return m == ScalarMarshalingBuiltin || m == ScalarMarshalingMethod || m == ScalarMarshalingExternal
}

// BuiltInScalarGoTypes maps each Go type the built-in GraphQL scalar registry
// already owns to the scalar it resolves to (PRD §26.4.1). Keys are in the
// qualified short form a resolved column carries ("uuid.UUID"), not the full
// import path.
//
// `api.graphql.scalars` is additions only: declaring an entry for one of these
// Go types is a config error. The registry carries the Null-wrapper pairing
// and the DecimalComparator narrowing alongside the binding, and neither is
// expressible in a ScalarBinding — a rebind would silently split a scalar pair
// and revert numeric columns to text comparators.
//
// This mirrors the key set of gen.builtInScalarRegistry, which holds the
// actual bindings. It lives here because config must not import gen and
// `sqlgen validate` has to reach the rule; the gen package pins the two in
// sync with a parity test.
var BuiltInScalarGoTypes = map[string]string{
	"types.JSON":          "JSON",
	"types.DateTime":      "DateTime",
	"types.NullDateTime":  "NullDateTime",
	"uuid.UUID":           "UUID",
	"uuid.NullUUID":       "NullUUID",
	"decimal.Decimal":     "Decimal",
	"decimal.NullDecimal": "NullDecimal",
	"json.RawMessage":     "JSON",
	"time.Time":           "Time",

	// Go types the built-in dialect tables reach that would otherwise have
	// no binding at all.
	"[]byte":           "Bytes",
	"time.Duration":    "Duration",
	"net.IP":           "IP",
	"net.IPNet":        "CIDR",
	"net.HardwareAddr": "MacAddr",

	// The `database/sql.NullX` wrappers every nullable
	// column resolves to under `overrides.use_pointers: false`. §26.4.1's
	// Null-wrapper pairing already named this family.
	"sql.NullString":  "NullString",
	"sql.NullBool":    "NullBool",
	"sql.NullInt16":   "NullInt16",
	"sql.NullInt32":   "NullInt32",
	"sql.NullInt64":   "NullInt64",
	"sql.NullFloat64": "NullFloat64",
	"sql.NullTime":    "NullTime",
}

// ReservedScalarNames lists the GraphQL scalar names `api.graphql.scalars`
// may not declare, mapped to the reason. Two groups, both fatal for the same
// underlying reason — sqlgen identifies a scalar by NAME downstream of the
// Go-type lookup:
//
//   - The built-in registry's own scalars. The §26.4.1 Null-wrapper pairing
//     and the DecimalComparator narrowing both key on the scalar name, and
//     scalar registration is first-write-wins by name. A consumer entry named
//     `Decimal` therefore does not merely add a scalar: it takes over the
//     registry's slot, so real `decimal.Decimal` columns inherit the
//     consumer's binding, drop out of the emitted marshaler set, and end up
//     anchored at a package that has no MarshalDecimal.
//   - The GraphQL spec built-ins. sqlgen emits `scalar <Name>` for every
//     scalar a column resolves to; emitting `scalar String` makes the
//     generated schema unparseable.
//
// gqlgen's other bundled scalars (Int64, Map, Upload, Any) are deliberately
// NOT reserved — declaring one with `marshaling: builtin` is the supported
// way to route a column onto them, for a Go type gqlgen's bundled marshaler
// actually carries (gqlgenBundledScalars).
//
// This is the scalar-name half of the same rule BuiltInScalarGoTypes enforces
// on the Go-type half; either axis alone leaves the other open.
var ReservedScalarNames = map[string]string{
	"JSON":         "a built-in scalar",
	"DateTime":     "a built-in scalar",
	"NullDateTime": "a built-in scalar",
	"UUID":         "a built-in scalar",
	"NullUUID":     "a built-in scalar",
	"Decimal":      "a built-in scalar",
	"NullDecimal":  "a built-in scalar",
	"Time":         "a built-in scalar",
	"Bytes":        "a built-in scalar",
	"Duration":     "a built-in scalar",
	"IP":           "a built-in scalar",
	"CIDR":         "a built-in scalar",
	"MacAddr":      "a built-in scalar",
	"NullString":   "a built-in scalar",
	"NullBool":     "a built-in scalar",
	"NullInt16":    "a built-in scalar",
	"NullInt32":    "a built-in scalar",
	"NullInt64":    "a built-in scalar",
	"NullFloat64":  "a built-in scalar",
	"NullTime":     "a built-in scalar",
	"String":       "a GraphQL spec built-in",
	"Int":          "a GraphQL spec built-in",
	"Float":        "a GraphQL spec built-in",
	"Boolean":      "a GraphQL spec built-in",
	"ID":           "a GraphQL spec built-in",
}

// GqlgenGraphQLPkg is the import path of gqlgen's runtime graphql package,
// where its bundled Marshal* / Unmarshal* functions live.
const GqlgenGraphQLPkg = "github.com/99designs/gqlgen/graphql"

// gqlgenBundledScalars maps every scalar gqlgen binds on its own when the
// generated schema DECLARES it — v0.17.90 `codegen/config/config.go::
// injectBuiltins`' `extraBuiltins` — to the Go types its bundled marshalers
// carry, and the symbol each is spelled by in GqlgenGraphQLPkg.
//
// `marshaling: builtin` says "gqlgen ships the marshaler", and sqlgen read
// that as "so no `models:` entry is needed". The premise held only by luck.
// gqlgen gives `Int64` a TWO-entry model list — `[graphql.Int, graphql.Int64]`
// — and a GENERATED position (create/update input field, resolver argument)
// takes `Model[0]`, so an `int64` column routed onto `Int64` got an `int`
// input field in front of an `int64` model and the input translator emitted a
// non-compiling assignment. Every other bundled scalar has a single-entry
// list whose Go type is the one a column would carry, which is why only
// `Int64` failed — and failed silently, because the READ side binds `Model[1]`
// and works.
//
// Naming the marshaler outright removes the list: `injectBuiltins` skips any
// key `c.Models.Exists`, so `Model[0]` IS the declared `go_type` in every
// position and no coercion is left to get wrong. That is also what makes this
// table immune to the `Model[0]` reordering the numeric-width anchors have to
// re-check on a gqlgen bump — pinning the symbol means there is no list order to depend on.
// A scalar gqlgen does NOT bundle is a different failure and is rejected
// rather than pinned: gqlgen leaves it unbound and silently binds it to
// `string` (a `string` input field and a `panic("not implemented")` field
// resolver).
//
// Keys are the QUALIFIED SHORT FORM (QualifiedGoType), which is how both a
// resolved column and BuiltInScalarGoTypes spell a Go type.
var gqlgenBundledScalars = map[string]map[string]string{
	"Int64":  {"int": "Int", "int64": "Int64"},
	"Time":   {"time.Time": "Time"},
	"Map":    {"map[string]any": "Map"},
	"Upload": {"graphql.Upload": "Upload"},
	"Any":    {"any": "Any"},
}

// canonicalGoType rewrites the `interface{}` spelling to `any` so one Go type
// has one key in gqlgenBundledScalars. Go treats the two as identical and a
// consumer may write either, in a bare `interface{}` or inside a composite
// (`map[string]interface{}`).
func canonicalGoType(goType string) string {
	return strings.ReplaceAll(goType, "interface{}", "any")
}

// GqlgenBundlesScalar reports whether gqlgen ships a marshaler for a scalar of
// this name at all — the precondition for `marshaling: builtin`.
func GqlgenBundlesScalar(scalar string) bool {
	_, ok := gqlgenBundledScalars[scalar]
	return ok
}

// GqlgenBundledMarshalerPath returns the gqlgen `models:` path pinning
// `scalar` to the bundled marshaler for `goType`, or empty string when gqlgen
// bundles no marshaler for that pair. goType is the qualified short form.
func GqlgenBundledMarshalerPath(scalar, goType string) string {
	symbol, ok := gqlgenBundledScalars[scalar][canonicalGoType(goType)]
	if !ok {
		return ""
	}
	return GqlgenGraphQLPkg + "." + symbol
}

// GqlgenBundledGoTypes lists, sorted, the Go types gqlgen's bundled marshaler
// for `scalar` can carry, so a rejection can name the way forward.
func GqlgenBundledGoTypes(scalar string) []string {
	return slices.Sorted(maps.Keys(gqlgenBundledScalars[scalar]))
}

// GqlgenBundledScalarNames lists, sorted, every scalar name gqlgen bundles a
// marshaler for.
func GqlgenBundledScalarNames() []string {
	return slices.Sorted(maps.Keys(gqlgenBundledScalars))
}

// graphQLNamePattern is the GraphQL spec's `Name` production. A scalar key
// becomes a `scalar <Name>` declaration in the generated schema, so a key
// that is not a valid Name yields a schema gqlgen cannot parse.
var graphQLNamePattern = regexp.MustCompile(`^[_A-Za-z][_0-9A-Za-z]*$`)

// IsValidGraphQLName reports whether s is a valid GraphQL identifier.
func IsValidGraphQLName(s string) bool {
	return graphQLNamePattern.MatchString(s)
}

// SplitGoType splits a fully-qualified Go type reference into its import path
// and type name, using the same last-dot rule as gqlgen's
// `internal/code.PkgAndType`. A type name never contains a dot, so the rule is
// exact even when the package path does. An unqualified name ("int64")
// returns an empty import path.
//
//	"net/netip.Addr"                        -> "net/netip", "Addr"
//	"github.com/shopspring/decimal.Decimal" -> "github.com/shopspring/decimal", "Decimal"
//	"gopkg.in/yaml.v3.Node"                 -> "gopkg.in/yaml.v3", "Node"
//	"int64"                                 -> "", "int64"
func SplitGoType(goType string) (importPath, typeName string) {
	i := strings.LastIndex(goType, ".")
	if i < 0 {
		return "", goType
	}
	return goType[:i], goType[i+1:]
}

// QualifiedGoType renders an import path and type name in the qualified short
// form a resolved column carries ("net/netip" + "Addr" -> "netip.Addr"), which
// is how BuiltInScalarGoTypes is keyed.
func QualifiedGoType(importPath, typeName string) string {
	if importPath == "" {
		return typeName
	}
	return packageNameForPath(importPath) + "." + typeName
}

// packageNameForPath is the package name an import path is expected to bind:
// its last element, with a module major-version element stripped first.
//
// The strip is what makes `github.com/gofrs/uuid/v5` render `uuid.UUID` rather
// than `v5.UUID`. `/vN` is a *module path* requirement from v2 on, not part of
// the package name — the package inside still declares `package uuid` — so the
// bare last element is the wrong answer for every v2+ module. Without it the
// §26.4.1 "owned by a built-in scalar" rule silently missed a `go_type`
// declared through such a module, and the two errors that do quote the
// qualified form named a package appearing nowhere in the consumer's config or
// code. No other supported integration is versioned this way
// (`github.com/google/uuid`, `github.com/shopspring/decimal`,
// `github.com/segmentio/ksuid`), which is why it went unseen until
// `github.com/gofrs/uuid/v5` became a documented UUID integration (PRD §7.4).
//
// This is a guess and can only be one: the real name lives in the package
// clause, and sqlgen never loads consumer packages (§7.1: "whether the named
// type exists is the compiler's verdict"). It is the same guess goimports and
// gqlgen make, and it is right for every path following the convention. The
// pre-modules `gopkg.in/yaml.v3` spelling carries its version *inside* the last
// element rather than as one of its own, so it is untouched here and still
// renders `yaml.v3` — no import in the built-in registry uses that form, and
// changing it would be a separate guess with no caller to justify it.
func packageNameForPath(importPath string) string {
	base := path.Base(importPath)
	if !isModuleMajorVersion(base) {
		return base
	}
	parent := path.Base(path.Dir(importPath))
	if parent == "." || parent == "/" {
		return base
	}
	return parent
}

// isModuleMajorVersion reports whether elem is a module major-version path
// element — "v2", "v3", "v11". "v0" and "v1" are never spelled in a module
// path, and a leading zero is not a major version, so neither is treated as
// one; that leaves a package genuinely named `v1` or `v0` alone.
func isModuleMajorVersion(elem string) bool {
	digits, ok := strings.CutPrefix(elem, "v")
	if !ok || digits == "" || digits == "0" || digits == "1" || digits[0] == '0' {
		return false
	}
	for i := range len(digits) {
		if digits[i] < '0' || digits[i] > '9' {
			return false
		}
	}
	return true
}

// ExtraTypeField defines a single field within an ExtraType.
type ExtraTypeField struct {
	Description string            `yaml:"description"`
	Type        string            `yaml:"type"`
	Import      string            `yaml:"import"`
	Tags        map[string]string `yaml:"tags"`
}

// Preset names for operations.
const (
	PresetAll          = "all"
	PresetReadOnly     = "read_only"
	PresetAppendOnly   = "append_only"
	PresetNoDelete     = "no_delete"
	PresetNoHardDelete = "no_hard_delete"
)

// validPresets is the set of recognized operations preset names.
var validPresets = map[string]bool{
	PresetAll:          true,
	PresetReadOnly:     true,
	PresetAppendOnly:   true,
	PresetNoDelete:     true,
	PresetNoHardDelete: true,
}

// ptrVal returns the value of a pointer, or the provided default if nil.
func ptrVal[T any](p *T, def T) T {
	if p != nil {
		return *p
	}
	return def
}

// defaultSoftDeleteColumns returns the default soft delete column detection list
// in priority order (highest first).
func defaultSoftDeleteColumns() []SoftDeleteConfig {
	return []SoftDeleteConfig{
		{Name: "deleted_at", Type: SoftDeleteTimestamp},
		{Name: "deleted_datetime", Type: SoftDeleteTimestamp},
		{Name: "is_deleted", Type: SoftDeleteBool},
		{Name: "deleted_flag", Type: SoftDeleteBool},
		{Name: "deleted", Type: SoftDeleteBool},
	}
}

// defaultUpdateColumns returns the default update column detection list.
func defaultUpdateColumns() []string {
	return []string{"updated_at", "updated_datetime", "last_modified_at", "modified_on"}
}

// ExpandPreset expands a preset name into a fully-populated Operations with all
// toggles explicitly set. Returns an error for unrecognized preset names.
//
// Presets exist only on the `api.operations` masks (PRD §26.5.1); the client
// has no operations block (§4.6). Every arm resolves every field: a preset is
// an explicit enumeration, not a partial overlay. A field an arm forgets stays
// nil, and every consumer dereferences, so the symptom is a nil-pointer panic
// rather than a wrong default. The client-only fields are resolved too, though
// no mask may name them and nothing reads them off a mask.
//
// Each …_with_related key follows the base it composes, which is what leaves
// append_only with create_with_related alone. Both delete presets keep all
// three, since a nested `disconnect` is an FK null-out on O2M, not a delete.
func ExpandPreset(preset string) (Operations, error) {
	t, f := new(true), new(false)

	switch preset {
	case PresetAll, "":
		return Operations{
			Preset: PresetAll,
			Get:    t, GetMany: t, Create: t, CreateMany: t,
			Update: t, UpdateMany: t, UpdateWhere: t, Upsert: t, UpsertMany: t,
			SoftDelete: t, Restore: t, HardDelete: t,
			Exists: t, Count: t, Increment: t,
			Paginate: t, Connection: t, Stream: t,
			CreateWithRelated: t, UpdateWithRelated: t, UpsertWithRelated: t,
		}, nil
	case PresetReadOnly:
		return Operations{
			Preset: PresetReadOnly,
			Get:    t, GetMany: t, Create: f, CreateMany: f,
			Update: f, UpdateMany: f, UpdateWhere: f, Upsert: f, UpsertMany: f,
			SoftDelete: f, Restore: f, HardDelete: f,
			Exists: t, Count: t, Increment: f,
			Paginate: t, Connection: t, Stream: t,
			CreateWithRelated: f, UpdateWithRelated: f, UpsertWithRelated: f,
		}, nil
	case PresetAppendOnly:
		return Operations{
			Preset: PresetAppendOnly,
			Get:    t, GetMany: t, Create: t, CreateMany: t,
			Update: f, UpdateMany: f, UpdateWhere: f, Upsert: f, UpsertMany: f,
			SoftDelete: f, Restore: f, HardDelete: f,
			Exists: t, Count: t, Increment: f,
			Paginate: t, Connection: t, Stream: t,
			CreateWithRelated: t, UpdateWithRelated: f, UpsertWithRelated: f,
		}, nil
	case PresetNoDelete:
		return Operations{
			Preset: PresetNoDelete,
			Get:    t, GetMany: t, Create: t, CreateMany: t,
			Update: t, UpdateMany: t, UpdateWhere: t, Upsert: t, UpsertMany: t,
			SoftDelete: f, Restore: f, HardDelete: f,
			Exists: t, Count: t, Increment: t,
			Paginate: t, Connection: t, Stream: t,
			CreateWithRelated: t, UpdateWithRelated: t, UpsertWithRelated: t,
		}, nil
	case PresetNoHardDelete:
		return Operations{
			Preset: PresetNoHardDelete,
			Get:    t, GetMany: t, Create: t, CreateMany: t,
			Update: t, UpdateMany: t, UpdateWhere: t, Upsert: t, UpsertMany: t,
			SoftDelete: t, Restore: t, HardDelete: f,
			Exists: t, Count: t, Increment: t,
			Paginate: t, Connection: t, Stream: t,
			CreateWithRelated: t, UpdateWithRelated: t, UpsertWithRelated: t,
		}, nil
	default:
		return Operations{}, fmt.Errorf("unknown operations preset: %q", preset)
	}
}

// ResolveOperations resolves an Operations value by expanding its preset and
// applying any individual toggle overrides on top.
func ResolveOperations(ops Operations) (Operations, error) {
	base, err := ExpandPreset(ops.Preset)
	if err != nil {
		return Operations{}, err
	}
	applyOperationOverrides(&base, ops)
	return base, nil
}

// applyOperationOverrides applies non-nil individual toggles from src onto dst.
func applyOperationOverrides(dst *Operations, src Operations) {
	overrideBool(&dst.Get, src.Get)
	overrideBool(&dst.GetMany, src.GetMany)
	overrideBool(&dst.Create, src.Create)
	overrideBool(&dst.CreateMany, src.CreateMany)
	overrideBool(&dst.Update, src.Update)
	overrideBool(&dst.UpdateMany, src.UpdateMany)
	overrideBool(&dst.UpdateWhere, src.UpdateWhere)
	overrideBool(&dst.Upsert, src.Upsert)
	overrideBool(&dst.UpsertMany, src.UpsertMany)
	overrideBool(&dst.SoftDelete, src.SoftDelete)
	overrideBool(&dst.Restore, src.Restore)
	overrideBool(&dst.HardDelete, src.HardDelete)
	overrideBool(&dst.Exists, src.Exists)
	overrideBool(&dst.Count, src.Count)
	overrideBool(&dst.Increment, src.Increment)
	overrideBool(&dst.Paginate, src.Paginate)
	overrideBool(&dst.Connection, src.Connection)
	overrideBool(&dst.Stream, src.Stream)
	overrideBool(&dst.CreateWithRelated, src.CreateWithRelated)
	overrideBool(&dst.UpdateWithRelated, src.UpdateWithRelated)
	overrideBool(&dst.UpsertWithRelated, src.UpsertWithRelated)
}

// overrideBool sets *dst to src if src is non-nil.
func overrideBool(dst **bool, src *bool) {
	if src != nil {
		*dst = src
	}
}

// LoadConfig reads the sqlgen configuration from the given path.
// If path is empty, it searches the working directory for sqlgen.yml or sqlgen.yaml.
// After loading, defaults are applied and environment variable overrides are processed.
func LoadConfig(path string) (*RootConfig, error) {
	data, err := readConfigFile(path)
	if err != nil {
		return nil, err
	}

	cfg, err := decodeConfig(data)
	if err != nil {
		return nil, err
	}

	applyDefaults(cfg)
	applyEnvOverrides(cfg)

	return cfg, nil
}

// decodeConfig unmarshals the config strictly: an unrecognized key is a config
// error (exit code 2, PRD §23.9) rather than a silently dropped line. A typo in
// a single key is otherwise invisible and can retarget the whole run — writing
// `output.path` leaves Output.Dir empty, which defaults to the working
// directory and takes every generated file, and every stale-artifact removal,
// with it.
//
// Known gap: keys nested inside a type with its own UnmarshalYAML (Operations,
// NullableVariant) are still accepted, because yaml.Node.Decode builds a fresh
// non-strict decoder that this setting cannot reach.
func decodeConfig(data []byte) (*RootConfig, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var cfg RootConfig
	// An empty document is a valid (if useless) config, and Decode reports it
	// as io.EOF where yaml.Unmarshal reported no error at all — keep the
	// permissive reading so validation, not the parser, names what is missing.
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return &cfg, nil
}

// readConfigFile reads config from the given path, or searches the working directory.
func readConfigFile(path string) ([]byte, error) {
	if path != "" {
		data, err := os.ReadFile(path) //nolint:gosec // config path is user-provided CLI input
		if err != nil {
			return nil, fmt.Errorf("reading config %s: %w", path, err)
		}
		return data, nil
	}

	// Search working directory for sqlgen.yml or sqlgen.yaml.
	for _, name := range []string{"sqlgen.yml", "sqlgen.yaml"} {
		data, err := os.ReadFile(name) //nolint:gosec // fixed filenames in working directory
		if err == nil {
			return data, nil
		}
	}

	return nil, fmt.Errorf("no sqlgen.yml or sqlgen.yaml found in working directory")
}

// applyDefaults sets all unset fields to their built-in default values.
func applyDefaults(cfg *RootConfig) {
	if cfg.Version == "" {
		cfg.Version = "v1"
	}
	applyInputDefaults(&cfg.Input)
	applyOutputDefaults(&cfg.Output)
	applyGenerationDefaults(&cfg.Generation)
	applyOverrideDefaults(&cfg.Overrides)
	applyCacheDefaults(cfg)
	applyTenancyDefaults(cfg)
	applyAPIDefaults(cfg)
	applyManifestDefaults(&cfg.Generation)
	applyMapDefaults(cfg)
}

// applyManifestDefaults fills unset fields on cfg.Generation.Manifest with
// the built-in defaults documented in PRD §30.2. Nothing is done when the
// YAML has no `manifest:` block (cfg.Generation.Manifest == nil) or when
// the block is present with `enabled: false` — defaults only apply once the
// consumer has opted in.
func applyManifestDefaults(gen *GenerationConfig) {
	m := gen.Manifest
	if m == nil {
		return
	}
	if m.Enabled == nil {
		m.Enabled = new(false)
	}
	if !*m.Enabled {
		return
	}
	if len(m.Formats) == 0 {
		m.Formats = []string{ManifestFormatJSON, ManifestFormatMarkdown}
	}
	if m.JSONFilename == "" {
		m.JSONFilename = "manifest_gen.json"
	}
	if m.JSONLayout == "" {
		m.JSONLayout = JSONLayoutSingle
	}
	if m.JSONPerEntityDir == "" {
		m.JSONPerEntityDir = "entities"
	}
	if m.MarkdownDir == "" {
		m.MarkdownDir = "manifest"
	}
	if m.IncludeExamples == nil {
		m.IncludeExamples = new(true)
	}
	if m.IncludeInternal == nil {
		m.IncludeInternal = new(false)
	}
	if m.EmbedInClient == nil {
		m.EmbedInClient = new(true)
	}
	if m.Breadcrumbs.ClaudeMD == nil {
		m.Breadcrumbs.ClaudeMD = new(true)
	}
	if m.Breadcrumbs.AgentsMD == nil {
		m.Breadcrumbs.AgentsMD = new(true)
	}
	if m.Breadcrumbs.PackageDoc == nil {
		m.Breadcrumbs.PackageDoc = new(true)
	}
	applyManifestMCPDefaults(m)
}

// applyManifestMCPDefaults fills unset fields on manifest.mcp with the
// MCP.md §3.4 defaults. Called only once manifest.enabled resolves true, so
// emit_project_config's default is unconditionally true here.
func applyManifestMCPDefaults(m *ManifestConfig) {
	if m.MCP == nil {
		m.MCP = &MCPConfig{}
	}
	if m.MCP.EmitProjectConfig == nil {
		m.MCP.EmitProjectConfig = new(true)
	}
	if len(m.MCP.ProjectConfigs) == 0 {
		m.MCP.ProjectConfigs = []string{".mcp.json"}
	}
	if m.MCP.ServerKey == "" {
		m.MCP.ServerKey = "sqlgen"
	}
}

// ResolveTableManifestEnabled applies the PRD §30.2 manifest resolution order
// for one table:
//
//  1. Table-level tables.<name>.manifest.enabled (if set).
//  2. Global generation.manifest.enabled.
//  3. Default false.
//
// Returns false when the global block is absent. The per-table override is
// opt-out only — a per-table true with global false is rejected by
// ValidatePreParse, so this resolver does not need to gate the table-level
// pointer on the global state.
func ResolveTableManifestEnabled(table TableConfig, global GenerationConfig) bool {
	globalEnabled := global.Manifest != nil && ptrVal(global.Manifest.Enabled, false)
	if table.Manifest != nil && table.Manifest.Enabled != nil {
		return *table.Manifest.Enabled
	}
	return globalEnabled
}

// applyAPIDefaults fills unset fields on cfg.API with the built-in defaults
// documented in PRD §26.3. Nothing is done when the YAML has no `api:` block.
func applyAPIDefaults(cfg *RootConfig) {
	if cfg.API == nil {
		return
	}
	if cfg.API.GraphQL == nil {
		cfg.API.GraphQL = &GraphQLAPIConfig{}
	}
	g := cfg.API.GraphQL
	// §26.5.8: schema_dir / resolver_dir are root-relative (paths from the
	// module root, like output.dir). When unset they default to a `graph`
	// subdirectory nested alongside the models package (`<output.dir>/graph`);
	// this computed default is what carries the full root-relative path, so
	// downstream import wiring joins it onto the module directly.
	if g.SchemaDir == "" {
		g.SchemaDir = filepath.Join(cfg.Output.Dir, "graph")
	}
	if g.ResolverDir == "" {
		g.ResolverDir = filepath.Join(cfg.Output.Dir, "graph")
	}
	if g.Package == "" {
		g.Package = derivePackageName(g.ResolverDir)
	}
	if g.MaxDepth == 0 {
		g.MaxDepth = 5
	}
	if g.MaxComplexity == 0 {
		g.MaxComplexity = 1000
	}
	if g.FieldCasing == "" {
		g.FieldCasing = FieldCasingCamel
	}
	if g.GqlgenConfig == "" {
		g.GqlgenConfig = "./gqlgen.yml"
	}
	if g.GqlgenBin == "" {
		g.GqlgenBin = "go run github.com/99designs/gqlgen"
	}
	if g.Scalars == nil {
		g.Scalars = map[string]ScalarBinding{}
	}
}

func applyInputDefaults(in *InputConfig) {
	if in.Dialect == "" {
		in.Dialect = DialectPostgres
	}
	if in.Source == "" {
		in.Source = SourceFiles
	}
	if in.Schema == "" {
		in.Schema = "*"
	}
	if len(in.Paths) == 0 {
		in.Paths = []string{"."}
	}
	if in.ParseMode == "" {
		in.ParseMode = ParseModeStrict
	}
}

func applyOutputDefaults(out *OutputConfig) {
	if out.Driver == "" {
		out.Driver = DriverPgx
	}
	if out.Dir == "" {
		out.Dir = "."
		out.dirDefaulted = true
	}
	if out.Layout == "" {
		out.Layout = LayoutSingleFile
	}
	if out.Package == "" {
		out.Package = derivePackageName(out.Dir)
	}
	applyOutputSubDefaults(out)
}

func applyOutputSubDefaults(out *OutputConfig) {
	if out.Enums == nil {
		out.Enums = &EnumOutputConfig{}
	}
	if out.Enums.File == "" {
		out.Enums.File = "enums_gen.go"
	}
	if out.Enums.Package == "" {
		out.Enums.Package = out.Package
	}
	if out.Types == nil {
		out.Types = &TypeOutputConfig{}
	}
	if out.Types.File == "" {
		out.Types.File = "types_gen.go"
	}
	if out.Types.Package == "" {
		out.Types.Package = out.Package
	}
	if out.Client == nil {
		out.Client = &ClientOutputConfig{}
	}
	if out.Client.Name == "" {
		out.Client.Name = "Client"
	}
	if out.Client.File == "" {
		out.Client.File = "client_gen.go"
	}
}

func applyGenerationDefaults(gen *GenerationConfig) {
	if gen.QueryLimit == nil {
		gen.QueryLimit = new(1000)
	}
	if gen.BatchSize == nil {
		gen.BatchSize = new(200)
	}
	if gen.PageSize == nil {
		gen.PageSize = new(100)
	}
	if len(gen.CursorKeys) == 0 {
		gen.CursorKeys = []string{"id"}
	}
	if gen.UUIDVersion == "" {
		gen.UUIDVersion = UUIDVersionV4
	}
	if gen.StrictUpdates == nil {
		gen.StrictUpdates = new(true)
	}
	if len(gen.SoftDeleteColumns) == 0 {
		gen.SoftDeleteColumns = defaultSoftDeleteColumns()
	}
	if len(gen.UpdateColumns) == 0 {
		gen.UpdateColumns = defaultUpdateColumns()
	}
}

func applyOverrideDefaults(ov *OverrideConfig) {
	if ov.UsePointers == nil {
		ov.UsePointers = new(true)
	}
	if ov.Types == nil {
		ov.Types = make(map[string]TypeOverride)
	}
}

// applyCacheDefaults fills unset fields on cfg.Cache with the built-in defaults
// documented in PRD §27.2 and CACHE.md §3.4. Nothing is done when the YAML has
// no `cache:` block. When `key_prefix` is empty (unset or explicit ""), the
// default "sqlgen" is applied and cfg.CacheKeyPrefixDefaulted is flipped so
// ValidatePreParse can emit the defaulted-key_prefix stderr warning.
func applyCacheDefaults(cfg *RootConfig) {
	if cfg.Cache == nil {
		return
	}
	c := cfg.Cache
	if c.TTL == "" {
		c.TTL = "1h"
	}
	if c.Serializer == "" {
		c.Serializer = SerializerJSON
	}
	if c.KeyPrefix == "" {
		c.KeyPrefix = "sqlgen"
		cfg.CacheKeyPrefixDefaulted = true
	}
	if c.Hydration == nil {
		c.Hydration = &HydrationConfig{Enabled: true, Timeout: "30s"}
	} else if c.Hydration.Timeout == "" {
		c.Hydration.Timeout = "30s"
	}
	if c.CircuitBreaker == nil {
		c.CircuitBreaker = &CircuitBreakerConfig{
			Enabled:           true,
			FailureThreshold:  5,
			ProbeInterval:     "30s",
			HalfOpenMaxProbes: 1,
		}
	}
}

func applyMapDefaults(cfg *RootConfig) {
	if cfg.Tables == nil {
		cfg.Tables = make(map[string]TableConfig)
	}
	if cfg.Views == nil {
		cfg.Views = make(map[string]ViewConfig)
	}
	if cfg.Extras == nil {
		cfg.Extras = make(map[string]ExtraType)
	}
}

// applyEnvOverrides applies environment variable overrides to the config.
func applyEnvOverrides(cfg *RootConfig) {
	if url := os.Getenv("SQLGEN_DB_URL"); url != "" {
		if cfg.Input.Connection == nil {
			cfg.Input.Connection = &ConnectionConfig{}
		}
		cfg.Input.Connection.URL = url
		// URL takes precedence over individual fields — clear them.
		cfg.Input.Connection.Host = ""
		cfg.Input.Connection.Port = 0
		cfg.Input.Connection.User = ""
		cfg.Input.Connection.Password = ""
		cfg.Input.Connection.Database = ""
	}

	if pw := os.Getenv("SQLGEN_DB_PASSWORD"); pw != "" {
		if cfg.Input.Connection == nil {
			cfg.Input.Connection = &ConnectionConfig{}
		}
		cfg.Input.Connection.Password = pw
	}
}

// derivePackageName extracts a Go package name from a directory path.
// It uses the last path component, lowercased, with hyphens replaced by underscores.
func derivePackageName(dir string) string {
	name := filepath.Base(dir)
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "-", "_")
	return name
}

// ResolveTableQueryLimit returns the effective query_limit for a table,
// applying the resolution order: table-level → global → built-in default.
func ResolveTableQueryLimit(table TableConfig, global GenerationConfig) int {
	return ptrVal(table.QueryLimit, ptrVal(global.QueryLimit, 1000))
}

// ResolveTableBatchSize returns the effective batch_size for a table.
func ResolveTableBatchSize(table TableConfig, global GenerationConfig) int {
	return ptrVal(table.BatchSize, ptrVal(global.BatchSize, 200))
}

// ResolveTablePageSize returns the effective page_size for a table.
func ResolveTablePageSize(table TableConfig, global GenerationConfig) int {
	return ptrVal(table.PageSize, ptrVal(global.PageSize, 100))
}

// CursorKeysDecision is the outcome of cursor_keys resolution for one table or
// view. It carries the effective keys, whether Connection should be emitted,
// an optional warning for fallback paths, and an error when an explicit
// override references missing columns. See PRD §4.13.
type CursorKeysDecision struct {
	// Keys is the effective cursor key column list. Empty when Emit is false.
	Keys []string
	// Emit reports whether Connection should be generated for this entity.
	Emit bool
	// Warning is a human-readable diagnostic when resolution relied on a
	// PK-less omit. Empty on clean resolution and on silent PK fallback.
	Warning string
	// Err is non-nil when an explicit table/view-level override references a
	// column that does not exist on the entity (hard validation error).
	Err error
}

// ResolveTableConnection applies the §4.13 cursor_keys resolution rules for a
// table. The rules are:
//
//  1. Explicit table-level cursor_keys with any missing column → Err.
//  2. Inherited (global) cursor_keys fully present → use them silently.
//  3. Inherited cursor_keys partially or fully missing, table has PK → fall
//     back to the PK column list silently (no warning).
//  4. Inherited cursor_keys partially or fully missing, table has no PK →
//     Emit is false and Warning is populated naming the table.
//
// qualifiedName is used in the warning / error message (e.g. "public.products").
// columns must list every column name on the table; pk must list the PK
// column names in declaration order.
func ResolveTableConnection(table TableConfig, global GenerationConfig, qualifiedName string, columns, pk []string) CursorKeysDecision {
	colSet := stringSet(columns)

	if len(table.CursorKeys) > 0 {
		if missing := missingColumns(table.CursorKeys, colSet); len(missing) > 0 {
			return CursorKeysDecision{
				Err: fmt.Errorf("cursor_keys: column %q does not exist in table %s", missing[0], qualifiedName),
			}
		}
		return CursorKeysDecision{Keys: table.CursorKeys, Emit: true}
	}

	inherited := global.CursorKeys
	if len(inherited) == 0 {
		inherited = []string{"id"}
	}
	if len(missingColumns(inherited, colSet)) == 0 {
		return CursorKeysDecision{Keys: inherited, Emit: true}
	}
	if len(pk) > 0 {
		return CursorKeysDecision{Keys: pk, Emit: true}
	}
	return CursorKeysDecision{
		Emit:    false,
		Warning: fmt.Sprintf("cursor_keys: inherited default does not match table %s and no primary key is available; Connection method omitted", qualifiedName),
	}
}

// ResolveViewConnection applies the §4.13 cursor_keys resolution rules for a
// view. The rules are:
//
//  1. Explicit view-level cursor_keys with any missing column → Err.
//  2. Inherited (global) cursor_keys fully present → use them silently.
//  3. Inherited cursor_keys partially or fully missing → Emit is false and
//     Warning is populated naming the view. Views have no PK fallback.
//
// qualifiedName is used in the warning / error message (e.g. "public.view_summary").
// columns must list every column name on the view.
func ResolveViewConnection(view ViewConfig, global GenerationConfig, qualifiedName string, columns []string) CursorKeysDecision {
	colSet := stringSet(columns)

	if len(view.CursorKeys) > 0 {
		if missing := missingColumns(view.CursorKeys, colSet); len(missing) > 0 {
			return CursorKeysDecision{
				Err: fmt.Errorf("cursor_keys: column %q does not exist in view %s", missing[0], qualifiedName),
			}
		}
		return CursorKeysDecision{Keys: view.CursorKeys, Emit: true}
	}

	inherited := global.CursorKeys
	if len(inherited) == 0 {
		inherited = []string{"id"}
	}
	if len(missingColumns(inherited, colSet)) == 0 {
		return CursorKeysDecision{Keys: inherited, Emit: true}
	}
	return CursorKeysDecision{
		Emit:    false,
		Warning: fmt.Sprintf("cursor_keys: inherited default does not match view %s; Connection method omitted (views have no primary key fallback)", qualifiedName),
	}
}

func stringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, v := range values {
		set[v] = true
	}
	return set
}

// missingColumns returns the subset of keys that are not present in colSet.
func missingColumns(keys []string, colSet map[string]bool) []string {
	var missing []string
	for _, k := range keys {
		if !colSet[k] {
			missing = append(missing, k)
		}
	}
	return missing
}

// ResolveTableStrictUpdates returns the effective strict_updates for a table.
func ResolveTableStrictUpdates(table TableConfig, global GenerationConfig) bool {
	return ptrVal(table.StrictUpdates, ptrVal(global.StrictUpdates, true))
}

// ResolveTableExcludeColumns returns the merged exclude_columns for a table
// (union of global + table-level lists).
func ResolveTableExcludeColumns(table TableConfig, global GenerationConfig) []string {
	seen := make(map[string]bool)
	var result []string
	for _, col := range global.ExcludeColumns {
		if !seen[col] {
			seen[col] = true
			result = append(result, col)
		}
	}
	for _, col := range table.ExcludeColumns {
		if !seen[col] {
			seen[col] = true
			result = append(result, col)
		}
	}
	return result
}

// ResolveTableEventsEnabled returns whether events are enabled for a table.
// Resolution order: table-level override → global events.enabled → false.
func ResolveTableEventsEnabled(table TableConfig, global *EventConfig) bool {
	if table.Events != nil && table.Events.Enabled != nil {
		return *table.Events.Enabled
	}
	if global != nil {
		return global.Enabled
	}
	return false
}

// IsValidPreset returns true if the given string is a recognized operations preset.
func IsValidPreset(s string) bool {
	return validPresets[s]
}

// ResolveTableCacheEnabled returns whether caching is enabled for a specific
// table. Resolution order: per-table override → global cache.enabled → false.
// A per-table `enabled: true` opts a table in even when the global
// `cache.enabled` is false.
func ResolveTableCacheEnabled(table TableConfig, global *CacheConfig) bool {
	if table.Cache != nil && table.Cache.Enabled != nil {
		return *table.Cache.Enabled
	}
	if global != nil {
		return global.Enabled
	}
	return false
}

// ResolveTableCacheTTL returns the effective TTL for a table's cache entries.
// Resolution order: per-table override → global → built-in default (1h).
// An unparseable duration string falls through to the next layer.
func ResolveTableCacheTTL(table TableConfig, global *CacheConfig) time.Duration {
	if table.Cache != nil && table.Cache.TTL != nil {
		if d, err := time.ParseDuration(*table.Cache.TTL); err == nil {
			return d
		}
	}
	if global != nil && global.TTL != "" {
		if d, err := time.ParseDuration(global.TTL); err == nil {
			return d
		}
	}
	return time.Hour
}

// ResolveViewCacheEnabled returns whether caching is enabled for a view.
// View caching is opt-in only — this returns true only when
// view.cache.enabled is explicitly set to true. Global cache.enabled is never
// inherited (PRD §27.11 / CACHE.md §3.3 rationale: views have no mutations, so
// silent inheritance would create TTL-only view caches).
func ResolveViewCacheEnabled(view ViewConfig, _ *CacheConfig) bool {
	if view.Cache == nil || view.Cache.Enabled == nil {
		return false
	}
	return *view.Cache.Enabled
}

// ResolveViewCacheTTL returns the effective TTL for a view's cache entries.
// Resolution order: per-view override → global → built-in default (1h).
func ResolveViewCacheTTL(view ViewConfig, global *CacheConfig) time.Duration {
	if view.Cache != nil && view.Cache.TTL != nil {
		if d, err := time.ParseDuration(*view.Cache.TTL); err == nil {
			return d
		}
	}
	if global != nil && global.TTL != "" {
		if d, err := time.ParseDuration(global.TTL); err == nil {
			return d
		}
	}
	return time.Hour
}

// MatchExcludeTablePattern reports whether the table name (or its
// schema-qualified form) matches any pattern in patterns. Patterns use the
// glob syntax of filepath.Match — `*` matches any sequence (excluding the
// schema separator), `?` matches a single character; case-sensitive.
// Schema-qualified forms `schema.table` are tested in addition to the bare
// table name so users can write `audit.*` to exclude an entire schema's
// tables. An empty patterns slice never matches.
func MatchExcludeTablePattern(name, schema string, patterns []string) (string, bool) {
	for _, p := range patterns {
		if matched, err := filepath.Match(p, name); err == nil && matched {
			return p, true
		}
		if schema != "" {
			qualified := schema + "." + name
			if matched, err := filepath.Match(p, qualified); err == nil && matched {
				return p, true
			}
		}
	}
	return "", false
}
