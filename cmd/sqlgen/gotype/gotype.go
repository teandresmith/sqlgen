// Package gotype provides SQL-to-Go type mapping with dialect awareness
// and override support. Used at generation time only.
package gotype

import (
	"maps"
	"slices"
	"strings"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype/decimal"
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
	"int": goInt32, "integer": goInt32, "mediumint": goInt32,
	"bigint":       goInt64,
	"int unsigned": goUint32, "bigint unsigned": goUint64,
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
	"bool": goBool, "boolean": goBool, "bit(1)": goBool,
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

// knownGoTypes maps Go type names to import path and zero value,
// used for resolving type_map literal Go type strings.
var knownGoTypes = map[string]struct{ imp, zero string }{
	"string":           {"", `""`},
	"int":              {"", "0"},
	"int8":             {"", "0"},
	"int16":            {"", "0"},
	"int32":            {"", "0"},
	"int64":            {"", "0"},
	"uint":             {"", "0"},
	"uint8":            {"", "0"},
	"uint16":           {"", "0"},
	"uint32":           {"", "0"},
	"uint64":           {"", "0"},
	"float32":          {"", "0"},
	"float64":          {"", "0"},
	"bool":             {"", "false"},
	"byte":             {"", "0"},
	"rune":             {"", "0"},
	"time.Time":        {"time", "time.Time{}"},
	"time.Duration":    {"time", "0"},
	"any":              {"", "nil"},
	"net.IP":           {"net", "nil"},
	"net.IPNet":        {"net", "net.IPNet{}"},
	"net.HardwareAddr": {"net", "nil"},
	"json.RawMessage":  {"encoding/json", "nil"},
	"types.JSON":       {"github.com/teandresmith/sqlgen/types", "nil"},
}

// Resolver resolves SQL types to Go types using the configured override chain.
type Resolver struct {
	dialect         config.Dialect
	usePointers     bool
	globalOverrides map[string]config.TypeOverride
	integrations    map[string]config.TypeOverride
	enums           map[string]string // SQL enum name → PascalCase Go type name
	sets            map[string]string // SQL set name → PascalCase Go set type name
	domains         map[string]string // SQL domain name → base SQL type
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
	r := &Resolver{
		dialect:         dialect,
		usePointers:     usePointers,
		globalOverrides: globalOverrides,
		integrations:    make(map[string]config.TypeOverride),
		enums:           make(map[string]string),
		sets:            make(map[string]string),
		domains:         make(map[string]string),
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

// RegisterEnum registers a schema enum type so that columns using this enum
// resolve to the generated Go type instead of falling through to string.
func (r *Resolver) RegisterEnum(sqlName, goTypeName string) {
	r.enums[strings.ToLower(sqlName)] = goTypeName
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

// RegisterSet registers a MySQL SET type so that columns using this set
// resolve to the generated Go set type (a named []ValueType slice).
func (r *Resolver) RegisterSet(sqlName, goTypeName string) {
	r.sets[strings.ToLower(sqlName)] = goTypeName
}

// IsSet reports whether the given SQL type name is a registered SET type.
func (r *Resolver) IsSet(sqlType string) bool {
	_, ok := r.sets[strings.ToLower(normalizeSQL(sqlType))]
	return ok
}

// RegisterDomain registers a schema domain type so that columns using this
// domain resolve to the domain's base SQL type through the normal chain.
func (r *Resolver) RegisterDomain(sqlName, baseSQLType string) {
	r.domains[strings.ToLower(sqlName)] = strings.ToLower(baseSQLType)
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
	t := sqlType
	for range 16 {
		base, ok := r.domains[t]
		if !ok {
			return t
		}
		t = base
	}
	return t
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
		case "tinyint", "smallint", "mediumint", "int", "integer", "bigint",
			"tinyint unsigned", "smallint unsigned", "mediumint unsigned",
			"int unsigned", "integer unsigned", "bigint unsigned":
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

// lookupOverride searches an override map for a SQL type, trying the exact
// type first and then falling back to the base type with modifiers stripped
// (e.g., numeric(10,2) → numeric).
func lookupOverride(m map[string]config.TypeOverride, sqlType string) (config.TypeOverride, bool) {
	if override, ok := m[sqlType]; ok {
		return override, true
	}
	if base := stripModifiers(sqlType); base != sqlType {
		if override, ok := m[base]; ok {
			return override, true
		}
	}
	return config.TypeOverride{}, false
}

// resolveByType implements steps 2-5 of the resolution chain.
func (r *Resolver) resolveByType(sqlType string, nullable bool, tableOverrides map[string]config.TypeOverride) GoType {
	// A table-scoped overrides.types map activates the integrations it names,
	// exactly as the global map does at resolver construction — nesting depth
	// is not part of the resolution (PRD §7.4). The two halves land at
	// different steps of the chain below: the scope's own entries are
	// overrides, what its integrations contribute for SQL types it did not
	// spell out is an integration.
	scopedOverrides, scopedIntegrations := scopeIntegrations(tableOverrides)

	// Step 2: Table-level overrides.types
	if override, ok := lookupOverride(scopedOverrides, sqlType); ok {
		return FromOverride(override, nullable)
	}

	// Step 3: Global overrides.types
	if override, ok := lookupOverride(r.globalOverrides, sqlType); ok {
		return FromOverride(override, nullable)
	}

	// Step 4: Built-in integrations. A table-scoped activation is the more
	// specific of the two, so it is consulted first; both sit below every
	// explicit override, per the chain in PRD §7.1.
	if override, ok := lookupOverride(scopedIntegrations, sqlType); ok {
		return FromOverride(override, nullable)
	}
	if override, ok := lookupOverride(r.integrations, sqlType); ok {
		return FromOverride(override, nullable)
	}

	// Step 4.5: Schema enums, sets, and domains
	if goTypeName, ok := r.enums[sqlType]; ok {
		gt := GoType{
			Name:      goTypeName,
			ZeroValue: `""`,
			FKConvert: FKStringSprint,
		}
		if nullable {
			gt.Name = "*" + goTypeName
			gt.ZeroValue = "nil"
		}
		return gt
	}
	if goTypeName, ok := r.sets[sqlType]; ok {
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
		}
	}
	if baseSQLType, ok := r.domains[sqlType]; ok {
		return r.resolveByType(baseSQLType, nullable, tableOverrides)
	}

	// Handle PostgreSQL array types: strip [] suffix and resolve base type.
	// Array columns always map to Go slices; nullable arrays use the same
	// slice type since slices are already reference types with a nil zero value.
	if r.dialect == config.DialectPostgres {
		if baseType, ok := strings.CutSuffix(sqlType, "[]"); ok {
			// Enum arrays use a named slice type (e.g., UserRoleSlice) with
			// Scan/Value methods so pgx can handle the custom OID without
			// manual type registration.
			if goTypeName, ok := r.enums[baseType]; ok {
				return GoType{
					Name:          goTypeName + "Slice",
					ZeroValue:     "nil",
					IsSlice:       true,
					SliceElemType: goTypeName,
					FKConvert:     FKStringSprint,
				}
			}

			base := r.resolveByType(baseType, false, tableOverrides)
			return GoType{
				Name:      "[]" + base.Name,
				Import:    base.Import,
				ZeroValue: "nil",
				IsSlice:   true,
				FKConvert: FKStringSprint,
			}
		}
	}

	// Step 5: Built-in dialect mapping
	return r.builtinResolve(sqlType, nullable)
}

// builtinResolve looks up the built-in dialect mapping for a SQL type.
func (r *Resolver) builtinResolve(sqlType string, nullable bool) GoType {
	if mappings, ok := dialectMappings[r.dialect]; ok {
		// Try exact match first (handles bit(1), etc.).
		if info, ok := mappings[sqlType]; ok {
			return r.goTypeFromInfo(info, nullable)
		}
		// Try stripping type modifiers: numeric(10,2) → numeric.
		if base := stripModifiers(sqlType); base != sqlType {
			if info, ok := mappings[base]; ok {
				return r.goTypeFromInfo(info, nullable)
			}
		}
	}

	// MySQL bit(N) where N > 1 maps to []byte.
	if r.dialect == config.DialectMySQL && strings.HasPrefix(sqlType, "bit(") && sqlType != "bit(1)" {
		return r.goTypeFromInfo(goBytes, nullable)
	}

	// Unknown type: default to string.
	return r.goTypeFromInfo(goString, nullable)
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
// It takes no resolver state, which is why it can be shared: an override names
// its own type, import and nullable variant outright, and nothing about the
// dialect, the schema's enums/domains, or the usePointers setting can change
// what it resolves to.
func FromOverride(override config.TypeOverride, nullable bool) GoType {
	zero := override.ZeroValue
	if zero == "" {
		zero = deriveZeroValue(override.Type)
	}

	gt := GoType{
		Name:      override.Type,
		Import:    override.Import,
		ZeroValue: zero,
		FKConvert: DeriveFKMethod(override.Type),
	}

	if !nullable {
		return gt
	}

	// Nullable with explicit nullable variant.
	if override.Nullable.Type != "" {
		gt.Name = override.Nullable.Type
		if override.Nullable.Import != "" {
			gt.Import = override.Nullable.Import
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

	// Value-typed override: wrap in pointer so the column's null state has
	// a Go-side representation distinct from the type's zero value.
	gt.Name = "*" + override.Type
	gt.ZeroValue = "nil"
	return gt
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
	info, ok := knownGoTypes[goType]
	return info.imp, ok
}

// FromLiteral resolves a literal Go type string to a GoType. Used for
// type_map entries and view columns whose type was pre-resolved to a Go type
// by @type annotations or aggregate inference.
func FromLiteral(goType string, nullable bool) GoType {
	gt := GoType{
		Name:      goType,
		FKConvert: DeriveFKMethod(goType),
	}

	if info, ok := knownGoTypes[goType]; ok {
		gt.Import = info.imp
		gt.ZeroValue = info.zero
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
