// Package gotype provides SQL-to-Go type mapping with dialect awareness
// and override support. Used at generation time only.
package gotype

import (
	"maps"
	"slices"
	"strings"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/decimal"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/literal"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/uuidgofrs"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/uuidgoogle"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/uuidstd"
)

// GoType represents a resolved Go type for a SQL column.
type GoType struct {
	Name          string         // Go type expression (e.g., "string", "*int32", "sql.NullString")
	Import        string         // import path (e.g., "", "database/sql", "time")
	ZeroValue     string         // zero value expression (e.g., `""`, "0", "nil")
	IsSlice       bool           // true for PostgreSQL array types mapped to Go slices
	SliceElemType string         // element type name for named slices — enum arrays ("UserRole") and MySQL SETs ("UsersPermissionsSetValue")
	FKConvert     FKStringMethod // how to convert to string for FK comparisons
	// UnmappedSQLType is set when no resolution step — override, integration,
	// enum, SET, domain or built-in mapping — maps the column's SQL type, so
	// it fell back to string. It holds the SQL type that missed, lowercased
	// and without modifiers (the element type for an array, the base for a
	// domain): the `overrides.types` key that would map it. Empty otherwise.
	UnmappedSQLType string
	// UnmappedComposite is the Go type name of the PostgreSQL composite type
	// UnmappedSQLType names, when it names one. The column still resolves to
	// string: the generated composite struct reads and writes JSON (PRD §8.3),
	// not the tuple text a composite column holds. Empty otherwise.
	UnmappedComposite string
}

// FKStringMethod indicates how to convert a value to string for FK comparisons.
type FKStringMethod int

const (
	// FKStringNone means the type is already a string.
	FKStringNone FKStringMethod = iota
	// FKStringStringer means the type has a .String() method.
	FKStringStringer
	// FKStringSprint means fmt.Sprint() should be used.
	FKStringSprint
)

// nullKind describes how a type handles nullability.
type nullKind int

const (
	nullSQL     nullKind = iota // has sql.Null* variant and supports *T
	nullRef                     // reference type: nullable uses the same type
	nullPtrOnly                 // only *T for nullable, no sql.Null* equivalent
)

// typeInfo holds built-in mapping information for a SQL type.
type typeInfo struct {
	name    string         // Go type name
	imp     string         // import path
	zero    string         // zero value expression
	null    string         // sql.Null* type name
	nullImp string         // import path for null type
	kind    nullKind       // nullability handling
	fk      FKStringMethod // FK string conversion method
}

// Common type definitions shared across dialects.
var (
	goString   = typeInfo{name: "string", zero: `""`, null: "sql.NullString", nullImp: "database/sql", kind: nullSQL}
	goInt8     = typeInfo{name: "int8", zero: "0", null: "sql.NullInt16", nullImp: "database/sql", kind: nullSQL, fk: FKStringSprint}
	goInt16    = typeInfo{name: "int16", zero: "0", null: "sql.NullInt16", nullImp: "database/sql", kind: nullSQL, fk: FKStringSprint}
	goInt32    = typeInfo{name: "int32", zero: "0", null: "sql.NullInt32", nullImp: "database/sql", kind: nullSQL, fk: FKStringSprint}
	goInt64    = typeInfo{name: "int64", zero: "0", null: "sql.NullInt64", nullImp: "database/sql", kind: nullSQL, fk: FKStringSprint}
	goUint8    = typeInfo{name: "uint8", zero: "0", null: "sql.NullInt16", nullImp: "database/sql", kind: nullSQL, fk: FKStringSprint}
	goUint16   = typeInfo{name: "uint16", zero: "0", null: "sql.NullInt32", nullImp: "database/sql", kind: nullSQL, fk: FKStringSprint}
	goUint32   = typeInfo{name: "uint32", zero: "0", null: "sql.NullInt64", nullImp: "database/sql", kind: nullSQL, fk: FKStringSprint}
	goUint64   = typeInfo{name: "uint64", zero: "0", null: "sql.NullInt64", nullImp: "database/sql", kind: nullSQL, fk: FKStringSprint}
	goFloat32  = typeInfo{name: "float32", zero: "0", null: "sql.NullFloat64", nullImp: "database/sql", kind: nullSQL, fk: FKStringSprint}
	goFloat64  = typeInfo{name: "float64", zero: "0", null: "sql.NullFloat64", nullImp: "database/sql", kind: nullSQL, fk: FKStringSprint}
	goBool     = typeInfo{name: "bool", zero: "false", null: "sql.NullBool", nullImp: "database/sql", kind: nullSQL, fk: FKStringSprint}
	goBytes    = typeInfo{name: "[]byte", zero: "nil", kind: nullRef, fk: FKStringSprint}
	goTime     = typeInfo{name: "time.Time", imp: "time", zero: "time.Time{}", null: "sql.NullTime", nullImp: "database/sql", kind: nullSQL, fk: FKStringStringer}
	goDuration = typeInfo{name: "time.Duration", imp: "time", zero: "0", kind: nullPtrOnly, fk: FKStringStringer}
	goUUID     = typeInfo{name: "uuid.UUID", imp: "uuid", zero: "uuid.UUID{}", kind: nullPtrOnly, fk: FKStringStringer}
	goJSON     = typeInfo{name: "types.JSON", imp: "github.com/teandresmith/sqlgen/types", zero: "nil", kind: nullRef, fk: FKStringSprint}
	goIP       = typeInfo{name: "net.IP", imp: "net", zero: "nil", kind: nullRef, fk: FKStringStringer}
	goIPNet    = typeInfo{name: "net.IPNet", imp: "net", zero: "net.IPNet{}", kind: nullPtrOnly, fk: FKStringStringer}
	goHWAddr   = typeInfo{name: "net.HardwareAddr", imp: "net", zero: "nil", kind: nullRef, fk: FKStringStringer}
)

// sqlNullEquivalents maps each Go type that has a database/sql null wrapper to
// the typeInfo the built-in mappings use for it, so an `overrides.types` entry
// naming that Go type without a `nullable:` variant takes the same wrapper a
// built-in column of that type takes under use_pointers false (PRD §4.7, §7.3).
var sqlNullEquivalents = map[string]typeInfo{
	goString.name: goString, goInt8.name: goInt8, goInt16.name: goInt16,
	goInt32.name: goInt32, goInt64.name: goInt64, goUint8.name: goUint8,
	goUint16.name: goUint16, goUint32.name: goUint32, goUint64.name: goUint64,
	goFloat32.name: goFloat32, goFloat64.name: goFloat64, goBool.name: goBool,
	goTime.name: goTime,
}

// postgresMappings maps PostgreSQL SQL types to Go types.
var postgresMappings = map[string]typeInfo{
	// String types
	"text": goString, "varchar": goString, "char": goString, "name": goString,
	// Integer types
	"int2": goInt16, "smallint": goInt16,
	"int4": goInt32, "integer": goInt32, "serial": goInt32,
	"int8": goInt64, "bigint": goInt64, "bigserial": goInt64,
	// Float types
	"float4": goFloat32, "real": goFloat32,
	"float8": goFloat64, "double precision": goFloat64,
	"numeric": goFloat64, "decimal": goFloat64,
	// Boolean
	"bool": goBool, "boolean": goBool,
	// Binary
	"bytea": goBytes,
	// Date/Time
	"timestamp": goTime, "timestamptz": goTime, "date": goTime,
	"time":     goString,
	"interval": goDuration,
	// UUID — the standard library binding is the default (PRD §7.4); the
	// two third-party libraries are reached through overrides.types.uuid.
	"uuid": goUUID,
	// JSON
	"json": goJSON, "jsonb": goJSON,
	// Network
	"inet": goIP, "cidr": goIPNet, "macaddr": goHWAddr,
}

// mysqlMappings maps MySQL SQL types to Go types.
var mysqlMappings = map[string]typeInfo{
	// Integer types
	"tinyint": goInt8, "smallint": goInt16,
	"int": goInt32, "mediumint": goInt32,
	"bigint":           goInt64,
	"tinyint unsigned": goUint8, "smallint unsigned": goUint16,
	"int unsigned": goUint32, "mediumint unsigned": goUint32,
	"bigint unsigned": goUint64,
	// Float types
	"float":  goFloat32,
	"double": goFloat64, "decimal": goFloat64,
	// String types
	"varchar": goString, "text": goString, "char": goString, "enum": goString,
	"tinytext": goString, "mediumtext": goString, "longtext": goString,
	"set": goString, "time": goString,
	// Date/Time
	"datetime": goTime, "timestamp": goTime, "date": goTime,
	// JSON
	"json": goJSON,
	// Binary types
	"blob": goBytes, "binary": goBytes, "varbinary": goBytes,
	"tinyblob": goBytes, "mediumblob": goBytes, "longblob": goBytes,
	// Boolean
	"tinyint(1)": goBool, "bit(1)": goBool,
	// Year
	"year": goInt16,
}

// sqliteMappings maps SQLite SQL types to Go types.
var sqliteMappings = map[string]typeInfo{
	// Integer types (SQLite maps all integer variants to int64)
	"integer": goInt64, "int": goInt64, "tinyint": goInt64,
	"smallint": goInt64, "bigint": goInt64,
	// String types
	"text": goString, "varchar": goString, "char": goString, "clob": goString,
	// Date/Time — SQLite has no date/time storage class; values are stored as TEXT.
	// types.DateTime is registered as an integration for datetime/timestamp/date
	// to provide time.Time semantics with NullDateTime for nullable columns.
	"datetime": goString, "timestamp": goString, "timestamptz": goString,
	"date": goString, "time": goString,
	// Float types
	"real": goFloat64, "float": goFloat64, "double": goFloat64,
	"numeric": goFloat64, "decimal": goFloat64,
	// Binary
	"blob": goBytes,
	// Boolean
	"boolean": goBool, "bool": goBool,
}

// dialectMappings returns the built-in type mapping table for each dialect.
var dialectMappings = map[config.Dialect]map[string]typeInfo{
	config.DialectPostgres: postgresMappings,
	config.DialectMySQL:    mysqlMappings,
	config.DialectSQLite:   sqliteMappings,
}

// Resolver resolves SQL types to Go types using the configured override chain.
type Resolver struct {
	dialect         config.Dialect
	usePointers     bool
	globalOverrides map[string]config.TypeOverride
	integrations    map[string]config.TypeOverride
	enums           map[string]string     // SQL enum name → PascalCase Go type name
	sets            map[string]string     // SQL set name → PascalCase Go set type name
	domains         map[string]domainBase // SQL domain name → base SQL type
	composites      map[string]string     // SQL composite type name → PascalCase Go struct name
	// bareOwners maps each bare enum, SET, domain or composite name to the schema that
	// registered it, and sharedBare holds the names more than one schema
	// registered. A shared name answers only to its qualified keys.
	bareOwners map[string]string
	sharedBare map[string]bool
	// ownerSchema is the schema of the table, view, composite or domain whose
	// types are being resolved (see ForSchema), or "" for none.
	ownerSchema string
	// userWrappers holds Null-wrapper extraction info for types declared in
	// sqlgen.yml overrides whose Nullable.UnderlyingField is set. Built-in
	// wrappers (sql.NullX, uuid.NullUUID, decimal.NullDecimal,
	// types.NullDateTime) live in knownWrapperExtractions and take precedence
	// to keep their shape stable across user configuration.
	userWrappers map[string]wrapperInfo
}

// integration pairs a known import path with the SQL types it handles and
// the TypeOverride it provides.
type integration struct {
	importPath string
	sqlTypes   []string
	override   config.TypeOverride
}

// knownIntegrations lists all built-in type integrations that can be
// auto-detected from global override import paths.
var knownIntegrations = []integration{
	{importPath: uuidstd.ImportPath, sqlTypes: uuidstd.SQLTypes(), override: uuidstd.Override()},
	{importPath: uuidgoogle.ImportPath, sqlTypes: uuidgoogle.SQLTypes(), override: uuidgoogle.Override()},
	{importPath: uuidgofrs.ImportPath, sqlTypes: uuidgofrs.SQLTypes(), override: uuidgofrs.Override()},
	{importPath: decimal.ImportPath, sqlTypes: decimal.SQLTypes(), override: decimal.Override()},
}

// NewResolver creates a Resolver for the given dialect and global configuration.
// It automatically detects known library imports in globalOverrides and registers
// the corresponding built-in type integrations.
func NewResolver(dialect config.Dialect, usePointers bool, globalOverrides map[string]config.TypeOverride) *Resolver {
	if globalOverrides == nil {
		globalOverrides = make(map[string]config.TypeOverride)
	}
	globalOverrides = lowerKeys(globalOverrides)
	r := &Resolver{
		dialect:         dialect,
		usePointers:     usePointers,
		globalOverrides: globalOverrides,
		integrations:    make(map[string]config.TypeOverride),
		enums:           make(map[string]string),
		sets:            make(map[string]string),
		domains:         make(map[string]domainBase),
		composites:      make(map[string]string),
		bareOwners:      make(map[string]string),
		sharedBare:      make(map[string]bool),
	}
	r.detectIntegrations()
	r.registerDialectDefaults()
	r.registerWrappersFromOverrides()
	return r
}

// dateTimeOverride is the TypeOverride for SQLite datetime columns using
// types.DateTime. Both types.DateTime and types.NullDateTime implement
// sql.Scanner/driver.Valuer, which is what PRD §4.7 requires of any
// overridden type.
var dateTimeOverride = config.TypeOverride{
	Type:      "types.DateTime",
	Import:    "github.com/teandresmith/sqlgen/types",
	ZeroValue: "types.DateTime{}",
	Nullable: config.NullableVariant{
		Type:            "types.NullDateTime",
		UnderlyingField: "DateTime",
	},
}

// registerDialectDefaults registers built-in integrations that are always
// active for a specific dialect (not dependent on user overrides).
func (r *Resolver) registerDialectDefaults() {
	if r.dialect != config.DialectSQLite {
		return
	}
	// SQLite has no native date/time storage class. Register types.DateTime
	// as the default for datetime/timestamp/date so that nullable columns
	// use types.NullDateTime instead of *string.
	for _, sqlType := range []string{"datetime", "timestamp", "timestamptz", "date"} {
		if _, ok := r.globalOverrides[sqlType]; ok {
			continue // user override takes precedence
		}
		if _, ok := r.integrations[sqlType]; ok {
			continue // already registered by another integration
		}
		r.RegisterIntegration(sqlType, dateTimeOverride)
	}
}

// detectIntegrations scans global overrides for known library import paths and
// registers the corresponding built-in type integrations. For SQL types the user
// explicitly overrode, integration defaults fill in unspecified fields. For SQL
// types covered by the integration but not overridden by the user, the integration
// is registered at step 4 of the resolution chain.
func (r *Resolver) detectIntegrations() {
	detected := detectIntegrationsIn(r.globalOverrides)

	// Iterate in a fixed order. Several integrations can claim one SQL type
	// — all three UUID libraries claim "uuid" — so when a config pulls in two
	// of them the registration below is a last-writer-wins race over map
	// iteration order, and the same config resolves to a different Go type
	// from run to run. Sorting does not decide which library *should* win for
	// a SQL type the user left unclaimed (that config is ambiguous, and is
	// rejected outright); it only keeps generation reproducible, per the
	// deterministic-output rule in PRD §5.7.
	for _, importPath := range slices.Sorted(maps.Keys(detected)) {
		integ := detected[importPath]
		for _, sqlType := range integ.sqlTypes {
			if userOverride, ok := r.globalOverrides[sqlType]; ok {
				r.globalOverrides[sqlType] = enrichOverride(userOverride, integ.override)
			} else {
				r.RegisterIntegration(sqlType, integ.override)
			}
		}
	}
}

// detectIntegrationsIn returns the known integrations an `overrides.types` map
// selects, keyed by import path. Detection is a property of the map's contents
// alone, never of where the map was written, so the global and table scopes
// share this one implementation and cannot drift apart (PRD §7.4, "Integration
// detection is scope-independent"). Returns nil — not an empty map — when the
// map selects none, which is the common case and lets callers skip their work
// without allocating.
func detectIntegrationsIn(overrides map[string]config.TypeOverride) map[string]integration {
	var detected map[string]integration
	for _, override := range overrides {
		for _, integ := range knownIntegrations {
			if override.Import != integ.importPath {
				continue
			}
			if detected == nil {
				detected = map[string]integration{}
			}
			detected[integ.importPath] = integ
		}
	}
	return detected
}

// scopeIntegrations applies integration detection to a table-scoped
// `overrides.types` map. It is the table-scoped twin of detectIntegrations,
// and splits its result the same two ways: `declared` is the scope's own
// entries with integration defaults filled into the ones the integration owns,
// and `activated` holds the bindings the integration contributes for SQL types
// the scope claims but did not spell out — the `decimal` half of a scope that
// only names `numeric`. The caller places them at steps 2 and 4 of the
// resolution chain respectively, which is where their global counterparts sit
// (PRD §7.1).
//
// The scope map is never mutated: it is the caller's `tables.<t>.overrides.types`
// straight off the parsed config, shared with every other reader of it, where
// r.globalOverrides is the resolver's own copy to enrich in place. Enrichment
// writes into a clone instead.
//
// Both maps are rebuilt on each call rather than cached on the Resolver. A
// table's overrides block is a handful of entries and the loop above it is a
// few string compares, so a cache keyed on map identity would buy nothing a
// generation run can measure while costing the resolver mutable state; and
// when the scope selects no integration at all — every table-scoped block in
// the example tree today — this returns the caller's own map with nothing
// allocated.
func scopeIntegrations(scope map[string]config.TypeOverride) (declared, activated map[string]config.TypeOverride) {
	detected := detectIntegrationsIn(scope)
	if len(detected) == 0 {
		return scope, nil
	}
	declared = maps.Clone(scope)
	activated = map[string]config.TypeOverride{}
	// Sorted for the same reason detectIntegrations sorts: two integrations
	// can claim one SQL type, and map order would make the winner vary run to
	// run (PRD §5.7).
	for _, importPath := range slices.Sorted(maps.Keys(detected)) {
		integ := detected[importPath]
		for _, sqlType := range integ.sqlTypes {
			if userOverride, ok := declared[sqlType]; ok {
				declared[sqlType] = enrichOverride(userOverride, integ.override)
			} else {
				activated[strings.ToLower(sqlType)] = integ.override
			}
		}
	}
	return declared, activated
}

// enrichOverride fills in empty fields of a user's override with values from
// the integration's override. Explicit user values always take precedence.
// Integration null types (e.g., uuid.NullUUID, decimal.NullDecimal) are always
// used for nullable columns because they are purpose-built for SQL scanning.
//
// When the user's Nullable.Type matches the integration's, missing extraction
// fields (UnderlyingField, ValidField, ValidMethod, ValidInvert) are filled
// from the integration so users do not have to repeat the FK-extraction
// metadata they already get from the integration declaration.
//
// An integration only enriches an override that belongs to it. Several
// integrations can claim the same SQL type — all three UUID libraries claim
// "uuid" — so the caller pairs by SQL type alone and may hand us a user
// override naming a different library. Enriching across that boundary writes
// one library's spellings onto another's import: a stdlib-bound `uuid` column
// would take google's `uuid.NullUUID` while still importing "uuid", which has
// no such identifier. An override naming no import at all is not a competing
// claim, so it stays eligible.
func enrichOverride(user, integ config.TypeOverride) config.TypeOverride {
	if user.Import != "" && user.Import != integ.Import {
		return user
	}
	if user.ZeroValue == "" {
		user.ZeroValue = integ.ZeroValue
	}
	if user.Nullable.Type == "" {
		user.Nullable = integ.Nullable
		return user
	}
	if user.Nullable.Type == integ.Nullable.Type {
		if user.Nullable.UnderlyingField == "" {
			user.Nullable.UnderlyingField = integ.Nullable.UnderlyingField
		}
		if user.Nullable.ValidField == "" && user.Nullable.ValidMethod == "" {
			user.Nullable.ValidField = integ.Nullable.ValidField
			user.Nullable.ValidMethod = integ.Nullable.ValidMethod
			user.Nullable.ValidInvert = integ.Nullable.ValidInvert
		}
	}
	return user
}

// RegisterIntegration adds a built-in type integration mapping.
// Integrations are checked after global overrides but before dialect defaults.
func (r *Resolver) RegisterIntegration(sqlType string, override config.TypeOverride) {
	r.integrations[strings.ToLower(sqlType)] = override
}

// RegisterEnum registers the enum schema.sqlName so that columns using it
// resolve to the generated Go type instead of falling through to string. The
// enum answers to schema.sqlName, and to sqlName alone while no other schema
// registers an enum, SET, domain or composite by that name (PRD §7.1). An empty schema
// registers the bare name only.
func (r *Resolver) RegisterEnum(schema, sqlName, goTypeName string) {
	registerSchemaType(r, r.enums, schema, sqlName, goTypeName)
}

// domainBase is a registered domain's base type and the schema the domain
// lives in, which a bare base type name is relative to.
type domainBase struct {
	base   string
	schema string
}

// registerSchemaType adds value to m under schema.name and, while no other
// schema has registered a type by that name, under name. A name a second
// schema registers is dropped from every type map; each schema's type keeps
// its qualified key.
func registerSchemaType[V any](r *Resolver, m map[string]V, schema, name string, value V) {
	schema, name = strings.ToLower(schema), strings.ToLower(name)
	if schema != "" {
		m[schema+"."+name] = value
	}
	if r.sharedBare[name] {
		return
	}
	if owner, ok := r.bareOwners[name]; ok && owner != schema {
		r.sharedBare[name] = true
		delete(r.enums, name)
		delete(r.sets, name)
		delete(r.domains, name)
		delete(r.composites, name)
		return
	}
	r.bareOwners[name] = schema
	m[name] = value
}

// UsePointers reports the resolver's `overrides.use_pointers` setting. A nil
// receiver reports the default, true.
func (r *Resolver) UsePointers() bool {
	return r == nil || r.usePointers
}

// IsEnumGoType reports whether goType is the Go type name of a registered
// schema enum. Such a type is a named string declared in the generated package,
// so an untyped string constant is assignable to it. A nil receiver knows no
// enums.
func (r *Resolver) IsEnumGoType(goType string) bool {
	if r == nil || goType == "" {
		return false
	}
	for _, name := range r.enums {
		if name == goType {
			return true
		}
	}
	return false
}

// RegisterSet registers the MySQL SET type schema.sqlName so that columns
// using it resolve to the generated Go set type (a named []ValueType slice).
// It answers to the same names as an enum (see RegisterEnum).
func (r *Resolver) RegisterSet(schema, sqlName, goTypeName string) {
	registerSchemaType(r, r.sets, schema, sqlName, goTypeName)
}

// IsSet reports whether the given SQL type name is a registered SET type.
func (r *Resolver) IsSet(sqlType string) bool {
	key, ok := r.schemaTypeKey(normalizeSQL(sqlType))
	if !ok {
		return false
	}
	_, ok = r.sets[key]
	return ok
}

// ForSchema returns a resolver for the types of a table, view, composite type
// or domain in schema. It resolves a bare enum, SET or domain name as
// schema.name first and as the bare name second, so a column whose type lives
// in its own schema resolves even when another schema has a type by the same
// name (PRD §7.1). Override and integration keys are tried as schema.type
// before type, for the same reason (see lookupOverride). Built-in mappings are
// unchanged.
func (r *Resolver) ForSchema(schema string) *Resolver {
	if r == nil {
		return nil
	}
	scoped := *r
	scoped.ownerSchema = strings.ToLower(schema)
	return &scoped
}

// schemaTypeKey returns the key the enum, SET, domain and composite maps
// hold a bare sqlType under for the owner schema: ownerSchema.sqlType when
// that is registered, sqlType otherwise. A qualified sqlType, or any sqlType for a
// resolver with no owner schema, is returned as given. It reports false when
// sqlType must not be looked up in those maps at all: a bare name the
// dialect's built-in mapping knows never falls back to another schema's type
// of that name, since the database resolves it to the built-in type
// (PRD §7.1).
func (r *Resolver) schemaTypeKey(sqlType string) (string, bool) {
	if r.ownerSchema == "" || strings.Contains(sqlType, ".") {
		return sqlType, true
	}
	qualified := r.ownerSchema + "." + sqlType
	_, isEnum := r.enums[qualified]
	_, isSet := r.sets[qualified]
	_, isDomain := r.domains[qualified]
	_, isComposite := r.composites[qualified]
	if isEnum || isSet || isDomain || isComposite {
		return qualified, true
	}
	if owner, ok := r.bareOwners[sqlType]; ok && owner != r.ownerSchema && r.isBuiltin(sqlType) {
		return "", false
	}
	return sqlType, true
}

// isBuiltin reports whether the dialect's built-in mapping knows sqlType.
func (r *Resolver) isBuiltin(sqlType string) bool {
	mappings := dialectMappings[r.dialect]
	for _, key := range lookupKeys(sqlType) {
		if _, ok := mappings[key]; ok {
			return true
		}
	}
	return false
}

// RegisterDomain registers the domain schema.sqlName so that columns using it
// resolve to the domain's base SQL type through the normal chain. It answers
// to the same names as an enum (see RegisterEnum). A bare baseSQLType that
// names an enum, SET or domain is taken to be in the domain's schema.
func (r *Resolver) RegisterDomain(schema, sqlName, baseSQLType string) {
	registerSchemaType(r, r.domains, schema, sqlName, domainBase{
		base:   strings.ToLower(baseSQLType),
		schema: strings.ToLower(schema),
	})
}

// RegisterComposite registers the PostgreSQL composite type schema.sqlName,
// whose generated struct is goTypeName. It answers to the same names as an
// enum (see RegisterEnum), and it takes part in the same type namespace, so a
// bare name is never read as another schema's enum, SET or domain of that
// name. A column of the composite still resolves to string: the struct reads
// and writes JSON (PRD §8.3), and the column holds tuple text. The resolved
// GoType names the struct in UnmappedComposite, so the caller can say so.
func (r *Resolver) RegisterComposite(schema, sqlName, goTypeName string) {
	registerSchemaType(r, r.composites, schema, sqlName, goTypeName)
}

// domainBaseType returns the base SQL type of the registered domain sqlType.
// A bare base is qualified with the domain's schema when that names a
// registered enum, SET or domain, since the base was written relative to it.
func (r *Resolver) domainBaseType(sqlType string) (string, bool) {
	d, ok := r.domains[sqlType]
	if !ok {
		return "", false
	}
	if d.schema == "" || strings.Contains(d.base, ".") {
		return d.base, true
	}
	elem, isArray := strings.CutSuffix(d.base, "[]")
	qualified := d.schema + "." + elem
	_, isEnum := r.enums[qualified]
	_, isSet := r.sets[qualified]
	_, isDomain := r.domains[qualified]
	if !isEnum && !isSet && !isDomain {
		return d.base, true
	}
	if isArray {
		qualified += "[]"
	}
	return qualified, true
}

// Resolve resolves a SQL column type to a Go type using the resolution chain:
//  1. Table-level type_map (columnName to Go type literal)
//  2. Table-level overrides.types (SQL type to TypeOverride)
//  3. Global overrides.types (SQL type to TypeOverride)
//  4. Built-in integrations (UUID, decimal), table-scoped before global
//  5. Schema enums and domains (registered via RegisterEnum/RegisterDomain)
//  6. Built-in dialect mapping
//
// Steps 2 and 3 both activate the integrations their map names, so identical
// override text resolves to an identical mapping at either scope (PRD §7.4).
func (r *Resolver) Resolve(sqlType string, nullable bool, columnName string, tableTypeMap map[string]string, tableOverrides map[string]config.TypeOverride) GoType {
	// Step 1: Table-level type_map (column-specific override)
	if tableTypeMap != nil {
		if goType, ok := tableTypeMap[columnName]; ok {
			return FromLiteral(goType, nullable)
		}
	}

	return r.resolveByType(normalizeSQL(sqlType), nullable, tableOverrides)
}

// ResolveAggregate resolves the Go type of a view column produced by a SQL
// aggregate, modeling the database's result-type widening. It mirrors Resolve
// (the column type_map still wins) but, for SUM, widens the domain-resolved
// base SQL type per the dialect before the normal resolution chain runs.
//
// SUM widens its result to avoid overflow, and the rules are dialect-specific:
// PostgreSQL SUM(smallint|integer) → bigint and SUM(bigint) → numeric; MySQL
// SUM(<any integer>) → decimal; SQLite already maps every integer to int64 (so
// no widening is needed). Without this, a view's SUM(integer) column would
// generate a too-narrow scan target (*int32) that overflows or fails to scan
// the bigint/decimal the database actually returns. MIN/MAX do not widen and
// resolve through the normal chain.
func (r *Resolver) ResolveAggregate(aggFunc, sqlType string, nullable bool, columnName string, tableTypeMap map[string]string, tableOverrides map[string]config.TypeOverride) GoType {
	// Step 1: column-specific type_map still wins (mirrors Resolve).
	if tableTypeMap != nil {
		if goType, ok := tableTypeMap[columnName]; ok {
			return FromLiteral(goType, nullable)
		}
	}

	base := normalizeSQL(sqlType)
	if strings.EqualFold(aggFunc, "SUM") {
		base = r.widenSumBase(stripModifiers(r.resolveDomainBase(base)))
	}
	// ARRAY_AGG is PostgreSQL-only, and so is the array branch in
	// resolveByType. On any other dialect an array SQL type would miss every
	// builtin mapping and land on builtinResolve's unknown-type string
	// fallback — binding the column to a type it cannot scan. Fail closed to
	// the same []any the parser uses when no element type is resolvable
	// (PRD 16.3).
	if strings.EqualFold(aggFunc, "ARRAY_AGG") && r.dialect != config.DialectPostgres {
		return FromLiteral("[]any", nullable)
	}
	return r.resolveByType(base, nullable, tableOverrides)
}

// BaseSQLType follows domain definitions to the underlying base SQL type
// (`email` → `text`), normalizing case and whitespace first. Callers that
// *classify* a column by its SQL type rather than resolve a Go type from it
// need this: a domain name places the column in no category at all, so a check
// keyed on the raw type silently passes for every domain-typed column.
func (r *Resolver) BaseSQLType(sqlType string) string {
	return r.resolveDomainBase(normalizeSQL(sqlType))
}

// resolveDomainBase follows domain definitions to the underlying base SQL type
// (e.g. positive_int → integer), so aggregate widening keys on the real base
// rather than the domain name. Bounded to guard against pathological cycles.
func (r *Resolver) resolveDomainBase(sqlType string) string {
	t, ok := r.schemaTypeKey(sqlType)
	if !ok {
		return sqlType
	}
	if _, ok := r.domainBaseType(t); !ok {
		return sqlType
	}
	for range 16 {
		base, ok := r.domainBaseType(t)
		if !ok {
			return t
		}
		// The base is read in the domain's own schema, so a built-in base
		// name never reaches another schema's type of that name.
		key, ok := r.ForSchema(r.domains[t].schema).schemaTypeKey(base)
		if !ok {
			return base
		}
		t = key
	}
	return t
}

// SumResultSQLType returns the SQL type a SUM over a column of sqlType
// returns: the widened base type where the dialect widens it (PostgreSQL
// SUM(integer) → bigint, through any domain), and sqlType itself otherwise.
func (r *Resolver) SumResultSQLType(sqlType string) string {
	base := stripModifiers(r.resolveDomainBase(normalizeSQL(sqlType)))
	if widened := r.widenSumBase(base); widened != base {
		return widened
	}
	return sqlType
}

// widenSumBase maps a SUM's base SQL type to the type PostgreSQL / MySQL
// actually return, so the resolved Go type matches the scanned value. Returns
// the base unchanged when the dialect does not widen it (all non-integer
// bases, and every SQLite type — SQLite integers already resolve to int64).
func (r *Resolver) widenSumBase(base string) string {
	switch r.dialect {
	case config.DialectPostgres:
		switch base {
		case "smallint", "int2", "integer", "int4":
			return "bigint"
		case "bigint", "int8":
			return "numeric"
		}
	case config.DialectMySQL:
		switch base {
		case "tinyint", "smallint", "mediumint", "int", "bigint",
			"tinyint unsigned", "smallint unsigned", "mediumint unsigned",
			"int unsigned", "bigint unsigned":
			// MySQL returns DECIMAL for SUM over any exact-value integer,
			// signed or unsigned — the unsigned variants would otherwise leave
			// a uint32/uint64 scan target too narrow for the returned decimal.
			return "decimal"
		}
	case config.DialectSQLite:
		// No widening: SQLite maps every integer variant to int64, which already
		// matches the value SUM(integer) yields.
	}
	return base
}

// lowerKeys returns m with its keys lower-cased, so an overrides.types key
// matches whatever its letter case, as SQL types are compared lower-cased.
// It returns m itself when every key is already lower-case. Two keys that
// differ only in case are a config error; should both reach here, the
// lower-case spelling wins.
func lowerKeys(m map[string]config.TypeOverride) map[string]config.TypeOverride {
	upper := false
	for k := range m {
		if k != strings.ToLower(k) {
			upper = true
			break
		}
	}
	if !upper {
		return m
	}
	out := make(map[string]config.TypeOverride, len(m))
	for _, k := range slices.Sorted(maps.Keys(m)) {
		lower := strings.ToLower(k)
		if _, taken := out[lower]; taken && k != lower {
			continue
		}
		out[lower] = m[k]
	}
	return out
}

// lookupOverride searches an override map for a SQL type, trying the exact
// type first and then falling back to the base type with modifiers stripped
// (e.g., numeric(10,2) → numeric). For a resolver scoped by ForSchema, a bare
// type is first looked up qualified with the owner schema: both schema sources
// write a type in the column's own schema bare, so without this a key written
// app.status_enum would never reach the app schema's own columns (PRD §7.1).
func (r *Resolver) lookupOverride(m map[string]config.TypeOverride, sqlType string) (config.TypeOverride, string, bool) {
	keys := lookupKeys(sqlType)
	if r.ownerSchema != "" && !strings.Contains(sqlType, ".") {
		qualified := make([]string, 0, 2*len(keys))
		for _, key := range keys {
			qualified = append(qualified, r.ownerSchema+"."+key)
		}
		keys = append(qualified, keys...)
	}
	for _, key := range keys {
		if override, ok := m[key]; ok {
			return override, key, true
		}
	}
	return config.TypeOverride{}, "", false
}

// mysqlIntegerBases are the integer bases whose unsigned form never falls
// back to the signed base: MySQL's, and SQLite's integer.
var mysqlIntegerBases = map[string]bool{
	"tinyint": true, "smallint": true, "mediumint": true, "int": true, "integer": true, "bigint": true,
}

// lookupKeys returns the keys a SQL type is matched under, in order: as
// written, then without its modifiers (numeric(10,2) → numeric). A MySQL
// unsigned type is matched next as <base> unsigned. An integer one stops
// there: a ZEROFILL integer keeps its width, so int(10) unsigned zerofill is
// matched as int unsigned, and never as the signed int. Any other one falls
// back to its signed base, which has the same Go type, so double unsigned
// zerofill is matched as double unsigned, then double.
func lookupKeys(sqlType string) []string {
	if before, _, ok := strings.Cut(sqlType, " unsigned"); ok {
		base := stripModifiers(before)
		keys := []string{sqlType}
		if unsigned := base + " unsigned"; unsigned != sqlType {
			keys = append(keys, unsigned)
		}
		if !mysqlIntegerBases[base] {
			keys = append(keys, base)
		}
		return keys
	}
	base := stripModifiers(sqlType)
	if base == sqlType {
		return []string{sqlType}
	}
	return []string{sqlType, base}
}

// OverrideSource reports the `overrides.types` key that gives sqlType its Go
// type, as written in the config: the key the resolution chain matched, which
// for a domain is whichever link of the domain chain carried the entry. It
// reports whether that key is in tableOverrides rather than the global map,
// and ok is false when no `overrides.types` entry resolves sqlType.
func (r *Resolver) OverrideSource(sqlType string, tableOverrides map[string]config.TypeOverride) (key string, tableScoped, ok bool) {
	_, source := r.resolveByTypeSource(normalizeSQL(sqlType), false, tableOverrides)
	return source.key, source.tableScoped, source.key != ""
}

// overrideSource is the `overrides.types` entry a resolution matched. The
// zero value means none did.
type overrideSource struct {
	key         string
	tableScoped bool
}

// resolveByType implements steps 2-5 of the resolution chain.
func (r *Resolver) resolveByType(sqlType string, nullable bool, tableOverrides map[string]config.TypeOverride) GoType {
	gt, _ := r.resolveByTypeSource(sqlType, nullable, tableOverrides)
	return gt
}

// resolveByTypeSource is resolveByType, also reporting the override entry the
// Go type came from.
func (r *Resolver) resolveByTypeSource(sqlType string, nullable bool, tableOverrides map[string]config.TypeOverride) (GoType, overrideSource) {
	// A table-scoped overrides.types map activates the integrations it names,
	// exactly as the global map does at resolver construction — nesting depth
	// is not part of the resolution (PRD §7.4). The two halves land at
	// different steps of the chain below: the scope's own entries are
	// overrides, what its integrations contribute for SQL types it did not
	// spell out is an integration.
	scopedOverrides, scopedIntegrations := scopeIntegrations(lowerKeys(tableOverrides))

	// Step 2: Table-level overrides.types
	if override, key, ok := r.lookupOverride(scopedOverrides, sqlType); ok {
		return FromOverride(override, nullable, r.usePointers), overrideSource{key: key, tableScoped: true}
	}

	// Step 3: Global overrides.types
	if override, key, ok := r.lookupOverride(r.globalOverrides, sqlType); ok {
		return FromOverride(override, nullable, r.usePointers), overrideSource{key: key}
	}

	// Step 4: Built-in integrations. A table-scoped activation is the more
	// specific of the two, so it is consulted first; both sit below every
	// explicit override, per the chain in PRD §7.1.
	if override, _, ok := r.lookupOverride(scopedIntegrations, sqlType); ok {
		return FromOverride(override, nullable, r.usePointers), overrideSource{}
	}
	if override, _, ok := r.lookupOverride(r.integrations, sqlType); ok {
		return FromOverride(override, nullable, r.usePointers), overrideSource{}
	}

	// Step 4.5: Schema enums, sets, domains and composites, owner schema first
	if gt, source, ok := r.resolveSchemaType(sqlType, nullable, tableOverrides); ok {
		return gt, source
	}

	// Handle PostgreSQL array types: strip [] suffix and resolve base type.
	// Array columns always map to Go slices; nullable arrays use the same
	// slice type since slices are already reference types with a nil zero value.
	if r.dialect == config.DialectPostgres {
		if baseType, ok := strings.CutSuffix(sqlType, "[]"); ok {
			// Enum arrays use a named slice type (e.g., UserRoleSlice) with
			// Scan/Value methods so pgx can handle the custom OID without
			// manual type registration.
			if goTypeName, ok := r.arrayEnumElem(baseType); ok {
				return GoType{
					Name:          goTypeName + "Slice",
					ZeroValue:     "nil",
					IsSlice:       true,
					SliceElemType: goTypeName,
					FKConvert:     FKStringSprint,
				}, overrideSource{}
			}

			base, source := r.resolveByTypeSource(baseType, false, tableOverrides)
			return GoType{
				Name:            "[]" + base.Name,
				Import:          base.Import,
				ZeroValue:       "nil",
				IsSlice:         true,
				FKConvert:       FKStringSprint,
				UnmappedSQLType: base.UnmappedSQLType,
				// An array of a composite is tuple text per element as well.
				UnmappedComposite: base.UnmappedComposite,
			}, source
		}
	}

	// Step 5: Built-in dialect mapping
	return r.builtinResolve(sqlType, nullable), overrideSource{}
}

// arrayEnumElem returns the enum Go type of a PostgreSQL array element type
// that names a registered enum. An enum element wins over overrides.types.
func (r *Resolver) arrayEnumElem(elemSQLType string) (string, bool) {
	key, schemaType := r.schemaTypeKey(elemSQLType)
	goTypeName, ok := r.enums[key]
	return goTypeName, schemaType && ok
}

// ArrayElemGoType returns the Go type of one element of a PostgreSQL array
// whose element SQL type is elemSQLType, as the array column resolves it
// when nothing retypes the column.
func (r *Resolver) ArrayElemGoType(elemSQLType string, tableOverrides map[string]config.TypeOverride) string {
	elemSQLType = normalizeSQL(elemSQLType)
	if goTypeName, ok := r.arrayEnumElem(elemSQLType); ok {
		return goTypeName
	}
	base, _ := r.resolveByTypeSource(elemSQLType, false, tableOverrides)
	return base.Name
}

// resolveSchemaType resolves sqlType as a registered enum, SET, domain or
// composite, owner schema first. It reports false when sqlType names none.
func (r *Resolver) resolveSchemaType(sqlType string, nullable bool, tableOverrides map[string]config.TypeOverride) (GoType, overrideSource, bool) {
	key, ok := r.schemaTypeKey(sqlType)
	if !ok {
		return GoType{}, overrideSource{}, false
	}
	if goTypeName, ok := r.enums[key]; ok {
		gt := GoType{
			Name:      goTypeName,
			ZeroValue: `""`,
			FKConvert: FKStringSprint,
		}
		if nullable {
			gt.Name = "*" + goTypeName
			gt.ZeroValue = "nil"
		}
		return gt, overrideSource{}, true
	}
	if goTypeName, ok := r.sets[key]; ok {
		// SET types are named slices (reference types), so nullable uses the same type.
		//
		// IsSlice stays false: the model side treats a SET as an opaque named
		// type (comparator.String, no pq.Array wrapping, its own Scan/Value).
		// SliceElemType still carries the element, which is what lets the API
		// layer project the column as a GraphQL enum list without re-deriving
		// the name from the slice type's spelling. Every model-side
		// reader of SliceElemType is gated on IsSlice, so this is inert there.
		return GoType{
			Name:          goTypeName,
			ZeroValue:     "nil",
			SliceElemType: goTypeName + "Value",
			FKConvert:     FKStringSprint,
		}, overrideSource{}, true
	}
	if d, ok := r.domains[key]; ok {
		// A domain's base resolves as a column of the domain, in the
		// domain's schema (PRD §7.1).
		scope := r
		if d.schema != "" && d.schema != r.ownerSchema {
			scope = r.ForSchema(d.schema)
		}
		gt, source := scope.resolveByTypeSource(d.base, nullable, tableOverrides)
		return gt, source, true
	}
	if goTypeName, ok := r.composites[key]; ok {
		gt := r.goTypeFromInfo(goString, nullable)
		gt.UnmappedSQLType = stripModifiers(sqlType)
		gt.UnmappedComposite = goTypeName
		return gt, overrideSource{}, true
	}
	return GoType{}, overrideSource{}, false
}

// builtinResolve looks up the built-in dialect mapping for a SQL type.
func (r *Resolver) builtinResolve(sqlType string, nullable bool) GoType {
	if mappings, ok := dialectMappings[r.dialect]; ok {
		// The exact key first handles bit(1).
		for _, key := range lookupKeys(sqlType) {
			if info, ok := mappings[key]; ok {
				return r.goTypeFromInfo(info, nullable)
			}
		}
	}

	// MySQL bit(N) where N > 1 maps to []byte.
	if r.dialect == config.DialectMySQL && strings.HasPrefix(sqlType, "bit(") && sqlType != "bit(1)" {
		return r.goTypeFromInfo(goBytes, nullable)
	}

	// Unknown type: default to string, and say so, so the caller can warn.
	gt := r.goTypeFromInfo(goString, nullable)
	gt.UnmappedSQLType = stripModifiers(sqlType)
	return gt
}

// goTypeFromInfo converts a typeInfo to a GoType, applying nullable handling.
func (r *Resolver) goTypeFromInfo(info typeInfo, nullable bool) GoType {
	if !nullable {
		return GoType{
			Name:      info.name,
			Import:    info.imp,
			ZeroValue: info.zero,
			FKConvert: info.fk,
		}
	}

	switch info.kind {
	case nullRef:
		// Reference types use the same type for nullable (nil is the zero value).
		return GoType{
			Name:      info.name,
			Import:    info.imp,
			ZeroValue: "nil",
			FKConvert: info.fk,
		}
	case nullPtrOnly:
		// Types with no sql.Null* equivalent always use pointer.
		return GoType{
			Name:      "*" + info.name,
			Import:    info.imp,
			ZeroValue: "nil",
			FKConvert: info.fk,
		}
	default: // nullSQL
		if r.usePointers {
			return GoType{
				Name:      "*" + info.name,
				Import:    info.imp,
				ZeroValue: "nil",
				FKConvert: info.fk,
			}
		}
		return GoType{
			Name:      info.null,
			Import:    info.nullImp,
			ZeroValue: info.null + "{}",
			FKConvert: info.fk,
		}
	}
}

// FromOverride converts a config.TypeOverride to a GoType, applying the whole
// §4.7 override shape: `type` / `import`, `zero_value` (derived when absent),
// `convert`, and the `nullable` variant.
//
// It is the package-level form of the resolution every `overrides.types` entry
// goes through, exported because `tenancy.type` is specified as "a TypeOverride
// (same shape as section 4.7)" (§29.2.4) and is applied to the tenant column
// outside the resolver's SQL-type-keyed chain — it is keyed on the *column*,
// not on a SQL type, so it cannot ride `resolveByType`. Routing it here rather
// than re-deriving a type from the literal is what makes "same shape" true
// instead of aspirational: otherwise a key §4.7 honors could be parsed for
// `tenancy.type` and quietly dropped.
//
// It takes no resolver state beyond usePointers, which is why it can be
// shared: an override names its own type, import and nullable variant, and
// nothing about the dialect or the schema's enums/domains can change what it
// resolves to. usePointers matters only for a nullable column whose override
// names no `nullable:` variant: an omitted variant "uses `*T` when
// `use_pointers` is true, or `sql.Null*` when false" (PRD §4.7), and only a Go
// type with a database/sql null equivalent has a `sql.Null*` to take (§7.3).
func FromOverride(override config.TypeOverride, nullable, usePointers bool) GoType {
	zero := override.ZeroValue
	if zero == "" {
		zero = deriveZeroValue(override.Type)
	}

	gt := GoType{
		Name:      override.Type,
		Import:    overrideImport(override.Type, override.Import),
		ZeroValue: zero,
		FKConvert: DeriveFKMethod(override.Type),
	}

	if !nullable {
		return gt
	}

	// Nullable with explicit nullable variant. Without an import of its own it
	// shares the parent's explicit one; failing that, a known literal brings
	// its own.
	if override.Nullable.Type != "" {
		gt.Name = override.Nullable.Type
		switch {
		case override.Nullable.Import != "":
			gt.Import = override.Nullable.Import
		case override.Import == "":
			if info, ok := literal.Lookup(override.Nullable.Type); ok {
				gt.Import = info.Import
			}
		}
		if strings.HasPrefix(override.Nullable.Type, "*") {
			gt.ZeroValue = "nil"
		} else {
			gt.ZeroValue = override.Nullable.Type + "{}"
		}
		return gt
	}

	// No explicit nullable variant. Natively-nilable types (slice / map /
	// pointer / chan / stdlib slice aliases like `json.RawMessage`) carry
	// their own nil-state and use the bare type — pointer-wrapping them
	// would diverge from gqlgen's slice-aware scalar handling and produce
	// a malformed `marshalO*ᚖ` wrapper in the gqlgen-emitted code.
	if isNativelyNilable(override.Type) {
		gt.ZeroValue = "nil"
		return gt
	}

	// use_pointers false: a Go type with a database/sql null wrapper takes
	// it, exactly as a built-in column of that type does.
	if info, ok := sqlNullEquivalents[override.Type]; ok && !usePointers {
		gt.Name = info.null
		gt.Import = info.nullImp
		gt.ZeroValue = info.null + "{}"
		return gt
	}

	// Value-typed override: wrap in pointer so the column's null state has
	// a Go-side representation distinct from the type's zero value.
	gt.Name = "*" + override.Type
	gt.ZeroValue = "nil"
	return gt
}

// overrideImport returns an override's import: the explicit one, or the
// registry's for a known literal (PRD §4.7). Leaving it to goimports would make
// the generated file depend on the consumer's module cache.
func overrideImport(goType, explicit string) string {
	if explicit != "" {
		return explicit
	}
	info, _ := literal.Lookup(goType)
	return info.Import
}

// KnownLiteral reports whether goType names a Go type literal the resolver
// can place without help, returning the import path it needs (empty for
// builtins). It is the registry `FromLiteral` consults, exposed so config
// surfaces that accept a bare type literal can reject one whose package
// would never be imported.
//
// A literal outside this set carries no import: `type_map` has nowhere to
// declare one, and `column_map.<col>.type` needs its sibling `.import`
// (PRD §8.5 "Column Overrides").
func KnownLiteral(goType string) (string, bool) {
	info, ok := literal.Lookup(goType)
	return info.Import, ok
}

// FromLiteral resolves a literal Go type string to a GoType. Used for
// type_map entries and view columns whose type was pre-resolved to a Go type
// by @type annotations or aggregate inference.
func FromLiteral(goType string, nullable bool) GoType {
	gt := GoType{
		Name:      goType,
		FKConvert: DeriveFKMethod(goType),
	}

	if info, ok := literal.Lookup(goType); ok {
		gt.Import = info.Import
		gt.ZeroValue = info.Zero
	} else {
		gt.ZeroValue = deriveZeroValue(goType)
	}

	if !nullable {
		return gt
	}

	// Natively-nilable types (slice / map / pointer / chan / stdlib slice
	// aliases like `json.RawMessage`) already carry their own nil-state.
	// Pointer-wrapping them would diverge from gqlgen's slice-aware scalar
	// handling and produce a malformed `marshalO*ᚖ` wrapper in
	// generated_gen.go.
	if isNativelyNilable(goType) {
		gt.ZeroValue = "nil"
		return gt
	}

	gt.Name = "*" + goType
	gt.ZeroValue = "nil"
	return gt
}

// normalizeSQL normalizes a SQL type string for mapping lookup.
func normalizeSQL(sqlType string) string {
	return strings.ToLower(strings.TrimSpace(sqlType))
}

// stripModifiers removes type modifiers like (10, 2) from numeric(10, 2)
// so the base type can match built-in dialect mappings.
func stripModifiers(sqlType string) string {
	if i := strings.IndexByte(sqlType, '('); i > 0 {
		base := sqlType[:i]
		// Preserve array suffix if present: varchar(255)[] → varchar[]
		if strings.HasSuffix(sqlType, "[]") {
			return base + "[]"
		}
		return base
	}
	return sqlType
}

// DeriveFKMethod determines the FK string conversion method for a Go type.
func DeriveFKMethod(goType string) FKStringMethod {
	if goType == "string" {
		return FKStringNone
	}

	// Types known to have a .String() method.
	switch {
	case strings.HasSuffix(goType, ".UUID"):
		return FKStringStringer
	case goType == "time.Time", goType == "time.Duration", goType == "types.DateTime":
		return FKStringStringer
	case goType == "net.IP", goType == "net.IPNet", goType == "net.HardwareAddr":
		return FKStringStringer
	}

	return FKStringSprint
}

// ScalarExtraction describes how to safely read a (possibly-null) Go value as
// the underlying scalar and convert it to a string for FK comparisons. The
// $v placeholder is substituted with the caller's variable expression at use
// time. Used by the O2M relationship loader to handle Null-wrapped
// FK columns whose .String() does not exist on the wrapper.
type ScalarExtraction struct {
	// GuardExpr is a boolean Go expression that evaluates to true when the
	// value is valid (non-null). Empty string means no guard is needed.
	GuardExpr string
	// UnwrapExpr returns the underlying scalar value. Always non-empty.
	UnwrapExpr string
	// StringMethod tells how to convert the unwrapped value to string.
	StringMethod FKStringMethod
}

// wrapperInfo is the resolver-side runtime form of a NullableVariant after
// defaults have been applied. Built-in wrappers populate this from
// knownWrapperExtractions; user-declared wrappers populate it from
// config.NullableVariant fields at Resolver init.
type wrapperInfo struct {
	guardExpr      string         // pre-rendered guard with $v placeholder
	underlyingExpr string         // unwrap expression with $v placeholder
	stringMethod   FKStringMethod // applied to the unwrapped value
}

// knownWrapperExtractions covers Null-wrapper types that exist regardless of
// whether the user opts into them — both database/sql.NullX and the built-in
// library integrations (google/uuid, gofrs/uuid, shopspring/decimal,
// sqlgen/types). Their shape is fixed so we can hard-code the extraction.
// stdNullUnderlying maps each database/sql null wrapper to the Go type of the
// value field it carries — NOT to the SQL column types that resolve to it.
// The two differ: `smallint` and `tinyint` both resolve to sql.NullInt16, and
// `real` resolves to sql.NullFloat64, so the inverse of the typeInfo tables is
// ambiguous. The wrapper's own field type is not: sql.NullInt16 carries an
// int16, sql.NullFloat64 a float64. That is the type a generic constraint has
// to be given for the wrapper's value.
var stdNullUnderlying = map[string]string{
	"sql.NullString":  "string",
	"sql.NullBool":    "bool",
	"sql.NullByte":    "byte",
	"sql.NullInt16":   "int16",
	"sql.NullInt32":   "int32",
	"sql.NullInt64":   "int64",
	"sql.NullFloat64": "float64",
	"sql.NullTime":    "time.Time",
}

// StdNullUnderlying returns the Go type a database/sql null wrapper wraps, and
// whether goType names such a wrapper.
//
// A caller that classifies a column by its Go type — picking a comparator
// family, a generic type argument — has to ask this first when
// overrides.use_pointers is false, because every nullable column of a nullSQL
// type arrives as the wrapper rather than as *T. Without it a nullable
// `double precision` column yields comparator.NullableNumber[sql.NullFloat64],
// which does not satisfy comparator.Numeric, and a nullable `boolean` column
// yields a string comparator.
func StdNullUnderlying(goType string) (string, bool) {
	underlying, ok := stdNullUnderlying[goType]
	return underlying, ok
}

var knownWrapperExtractions = map[string]wrapperInfo{
	// database/sql.NullX
	"sql.NullString":  {guardExpr: "$v.Valid", underlyingExpr: "$v.String", stringMethod: FKStringNone},
	"sql.NullInt16":   {guardExpr: "$v.Valid", underlyingExpr: "$v.Int16", stringMethod: FKStringSprint},
	"sql.NullInt32":   {guardExpr: "$v.Valid", underlyingExpr: "$v.Int32", stringMethod: FKStringSprint},
	"sql.NullInt64":   {guardExpr: "$v.Valid", underlyingExpr: "$v.Int64", stringMethod: FKStringSprint},
	"sql.NullFloat64": {guardExpr: "$v.Valid", underlyingExpr: "$v.Float64", stringMethod: FKStringSprint},
	"sql.NullBool":    {guardExpr: "$v.Valid", underlyingExpr: "$v.Bool", stringMethod: FKStringSprint},
	"sql.NullTime":    {guardExpr: "$v.Valid", underlyingExpr: "$v.Time", stringMethod: FKStringStringer},
	"sql.NullByte":    {guardExpr: "$v.Valid", underlyingExpr: "$v.Byte", stringMethod: FKStringSprint},
	// Built-in integrations — same import paths as knownIntegrations + dateTimeOverride.
	"uuid.NullUUID":       {guardExpr: "$v.Valid", underlyingExpr: "$v.UUID", stringMethod: FKStringStringer},
	"decimal.NullDecimal": {guardExpr: "$v.Valid", underlyingExpr: "$v.Decimal", stringMethod: FKStringStringer},
	"types.NullDateTime":  {guardExpr: "$v.Valid", underlyingExpr: "$v.Time", stringMethod: FKStringStringer},
}

// DeriveScalarExtraction returns the ScalarExtraction for a Go type. Lookup
// order: user-declared wrappers (from sqlgen.yml overrides), built-in known
// wrappers (sql.NullX + integrations), pointer types (*T), bare types. The
// receiver may be nil — in that case only the built-in lookup paths apply,
// which is sufficient for tests and unit fixtures that do not exercise
// custom wrappers.
func (r *Resolver) DeriveScalarExtraction(goType string) ScalarExtraction {
	if r != nil {
		if w, ok := r.userWrappers[goType]; ok {
			return ScalarExtraction{
				GuardExpr:    w.guardExpr,
				UnwrapExpr:   w.underlyingExpr,
				StringMethod: w.stringMethod,
			}
		}
	}
	if w, ok := knownWrapperExtractions[goType]; ok {
		return ScalarExtraction{
			GuardExpr:    w.guardExpr,
			UnwrapExpr:   w.underlyingExpr,
			StringMethod: w.stringMethod,
		}
	}
	if bare, ok := strings.CutPrefix(goType, "*"); ok {
		// Wrap the deref in parens so applying `.String()` to the unwrapped
		// value binds correctly: `(*$v).String()` not `*$v.String()`.
		return ScalarExtraction{
			GuardExpr:    "$v != nil",
			UnwrapExpr:   "(*$v)",
			StringMethod: DeriveFKMethod(bare),
		}
	}
	return ScalarExtraction{UnwrapExpr: "$v", StringMethod: DeriveFKMethod(goType)}
}

// buildGuardExpr renders the guard expression for a NullableVariant, applying
// the ValidField default ("Valid") and ValidInvert negation. Returns the
// expression with $v as the variable placeholder.
func buildGuardExpr(n config.NullableVariant) string {
	switch {
	case n.ValidMethod != "" && n.ValidInvert:
		return "!$v." + n.ValidMethod + "()"
	case n.ValidMethod != "":
		return "$v." + n.ValidMethod + "()"
	case n.ValidField != "":
		return "$v." + n.ValidField
	default:
		return "$v.Valid"
	}
}

// registerWrappersFromOverrides walks every loaded TypeOverride whose
// Nullable.Type and Nullable.UnderlyingField are both populated, deriving a
// wrapperInfo for the wrapper Go type. Built-ins in knownWrapperExtractions
// always win over user-declared entries with the same Go type name.
func (r *Resolver) registerWrappersFromOverrides() {
	r.userWrappers = map[string]wrapperInfo{}
	for _, override := range r.globalOverrides {
		r.maybeRegisterWrapper(override)
	}
	for _, integ := range r.integrations {
		r.maybeRegisterWrapper(integ)
	}
}

// maybeRegisterWrapper adds an entry to r.userWrappers for the given override
// if it declares a Nullable wrapper with an underlying field set. The
// underlying type for StringMethod derivation is the override's bare Type.
func (r *Resolver) maybeRegisterWrapper(o config.TypeOverride) {
	if o.Nullable.Type == "" || o.Nullable.UnderlyingField == "" {
		return
	}
	if _, ok := knownWrapperExtractions[o.Nullable.Type]; ok {
		return
	}
	r.userWrappers[o.Nullable.Type] = wrapperInfo{
		guardExpr:      buildGuardExpr(o.Nullable),
		underlyingExpr: "$v." + o.Nullable.UnderlyingField,
		stringMethod:   DeriveFKMethod(o.Type),
	}
}

// deriveZeroValue infers the zero value expression for a Go type name.
func deriveZeroValue(typeName string) string {
	switch typeName {
	case "string":
		return `""`
	case "bool":
		return "false"
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"float32", "float64", "byte", "rune",
		"time.Duration":
		return "0"
	}

	if isNativelyNilable(typeName) {
		return "nil"
	}

	// Default: assume struct type.
	return typeName + "{}"
}

// isNativelyNilable reports whether typeName names a Go type that is
// already nil-comparable on its own — slices, maps, pointers, channels, and
// stdlib slice/map aliases (`json.RawMessage`, `net.IP`,
// `net.HardwareAddr`, `any`). Nullable columns whose Go type is natively
// nilable do NOT receive a `*T` wrapper at the model layer: the type's own
// nil-state carries the column's null semantics. Skipping the pointer wrap
// keeps the model row struct shape compatible with gqlgen's slice-aware
// scalar handling (FIX-F: gqlgen's pointer-wrapper auto-gen omits the
// address-of/deref for custom scalars whose underlying Go type is a slice
// alias, producing a malformed `marshalO*ᚖ`/`unmarshalO*ᚖ` body).
func isNativelyNilable(typeName string) bool {
	if strings.HasPrefix(typeName, "*") || strings.HasPrefix(typeName, "[]") || strings.HasPrefix(typeName, "map[") || strings.HasPrefix(typeName, "chan ") {
		return true
	}
	switch typeName {
	case "json.RawMessage", "types.JSON", "net.IP", "net.HardwareAddr", "any":
		return true
	}
	return false
}
