package gen

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// CacheRenderCtx carries the resolved global cache settings baked into
// cache_gen.go at codegen time. Config drift between sqlgen.yml and deployed
// behavior is avoided by materializing every runtime default here rather than
// accepting them at NewCache construction.
type CacheRenderCtx struct {
	Prefix               string
	Version              int
	DefaultTTL           time.Duration
	Serializer           config.Serializer
	SerializerID         string // "json" | "msgpack" | "custom" — pre-computed for template eq comparisons
	HydrationEnabled     bool
	HydrationTimeout     time.Duration
	BreakerEnabled       bool
	BreakerFailThreshold int
	BreakerProbeInterval time.Duration
	BreakerHalfOpenMax   int
}

// CachedPKColumn is one primary-key column for a cached table.
type CachedPKColumn struct {
	Name      string // SQL column name
	FieldName string // PascalCase Go field name on the row struct / composite PK struct
	GoType    string // resolved Go type expression
}

// CachedTable holds per-table data the cache template emits for every cached
// table. Every field is precomputed so the template stays declarative.
type CachedTable struct {
	Name                   string // SQL table name
	Struct                 string // Go struct name (e.g., "Product")
	Schema                 string // "public" for PostgreSQL, "" for MySQL/SQLite
	TableNameConstant      string // generated hook.TableName constant (e.g., "TableProducts")
	FieldOptionsName       string // e.g., "ProductFieldOptions"
	CompositePK            bool
	CompositePKStruct      string           // composite PK struct name (e.g., "OrderItemPK") when CompositePK
	PKGoType               string           // scalar Go type when single-PK
	PKColumns              []CachedPKColumn // in DDL PRIMARY KEY order
	ColumnFieldNames       []string         // PascalCase Go field names for all columns (for fullParentFieldOptions)
	RelationshipFieldNames []string         // PascalCase Go field names for all relationship-typed FieldOptions fields
	TTL                    time.Duration
	Fingerprint            string
	// Tenanted is true when the table participates in tenancy auto-filtering
	// (PRD §29.2). Drives the §29.5 tenant-segment-in-cache-key generation.
	Tenanted bool
	// TenantGoType is the resolved Go type expression for the tenant column
	// (e.g. "uuid.UUID"). Empty when Tenanted is false.
	TenantGoType string
	// TenantInPK is true when the tenant column is part of the primary key
	// (PRD §29.7). Invalidation then reads the tenant from the PK struct /
	// entity PK field instead of the AffectedTenants carrier.
	TenantInPK bool
	// TenantFieldName is the PascalCase Go field name of the tenant column
	// (e.g. "WorkspaceID"). Empty when Tenanted is false.
	TenantFieldName string
}

// CachedView holds per-view data for the cache template.
type CachedView struct {
	Name              string
	Struct            string
	Schema            string
	TableNameConstant string
	TTL               time.Duration
	InvalidateOn      []string // source table SQL names (sorted)
	Fingerprint       string
}

// CacheContext is the render context for cache.go.tmpl. Produced by
// BuildCacheContext; nil when caching is globally disabled.
type CacheContext struct {
	Package           string
	ClientName        string
	Imports           []string
	Config            CacheRenderCtx
	CachedTables      []CachedTable
	CachedViews       []CachedView
	ViewInvalidateMap map[string][]string // source table hook.TableName value -> list of view BuildTablePattern strings
}

// BuildCacheContext builds the cache context from table + view contexts and
// the resolved root config. Returns nil when the global cache is disabled.
func BuildCacheContext(tables []TableContext, views []ViewContext, cfg *config.RootConfig, pkg, clientName string) *CacheContext {
	if cfg.Cache == nil || !cfg.Cache.Enabled {
		return nil
	}

	cacheCfg := buildCacheRenderCtx(cfg.Cache)

	cachedTables := make([]CachedTable, 0, len(tables))
	var typeImports []string
	for _, t := range tables {
		tc := findTableConfig(cfg, t.TableName, t.Schema)
		if !config.ResolveTableCacheEnabled(tc, cfg.Cache) {
			continue
		}
		cachedTables = append(cachedTables, buildCachedTable(t, tc, cfg))
		typeImports = append(typeImports, cachedTableTypeImports(t)...)
	}

	slices.SortFunc(cachedTables, func(a, b CachedTable) int {
		return strings.Compare(a.Struct, b.Struct)
	})

	cachedViews := make([]CachedView, 0, len(views))
	for _, v := range views {
		vc := findViewConfig(cfg, v.ViewName, v.Schema)
		if !config.ResolveViewCacheEnabled(vc, cfg.Cache) {
			continue
		}
		cachedViews = append(cachedViews, buildCachedView(v, vc, cfg))
	}

	slices.SortFunc(cachedViews, func(a, b CachedView) int {
		return strings.Compare(a.Struct, b.Struct)
	})

	viewInvalidateMap := buildViewInvalidateMap(cachedViews, tables, cacheCfg.Prefix)

	imports := buildCacheImports(cacheCfg.Serializer, typeImports)

	return &CacheContext{
		Package:           pkg,
		ClientName:        clientName,
		Imports:           imports,
		Config:            cacheCfg,
		CachedTables:      cachedTables,
		CachedViews:       cachedViews,
		ViewInvalidateMap: viewInvalidateMap,
	}
}

// buildCacheRenderCtx resolves global cache config into the codegen-time
// render context. Defaults are applied by config.applyCacheDefaults earlier;
// duration strings that failed parsing fall back to safe values.
func buildCacheRenderCtx(c *config.CacheConfig) CacheRenderCtx {
	ctx := CacheRenderCtx{
		Prefix:       c.KeyPrefix,
		Version:      c.Version,
		Serializer:   c.Serializer,
		SerializerID: serializerID(c.Serializer),
		DefaultTTL:   parseDurationOrDefault(c.TTL, time.Hour),
	}
	if c.Hydration != nil {
		ctx.HydrationEnabled = c.Hydration.Enabled
		ctx.HydrationTimeout = parseDurationOrDefault(c.Hydration.Timeout, 30*time.Second)
	}
	if c.CircuitBreaker != nil {
		ctx.BreakerEnabled = c.CircuitBreaker.Enabled
		ctx.BreakerFailThreshold = c.CircuitBreaker.FailureThreshold
		ctx.BreakerProbeInterval = parseDurationOrDefault(c.CircuitBreaker.ProbeInterval, 30*time.Second)
		ctx.BreakerHalfOpenMax = c.CircuitBreaker.HalfOpenMaxProbes
	}
	return ctx
}

func buildCachedTable(t TableContext, tc config.TableConfig, cfg *config.RootConfig) CachedTable {
	schema := normalizeCacheSchema(t.Schema, cfg.Input.Dialect)

	pkCols := make([]CachedPKColumn, 0, len(t.PKColumns))
	for _, col := range t.PKColumns {
		pkCols = append(pkCols, CachedPKColumn{
			Name:      col.Name,
			FieldName: col.FieldName,
			GoType:    col.GoType,
		})
	}

	columnFields := make([]string, 0, len(t.Columns))
	for _, col := range t.Columns {
		columnFields = append(columnFields, col.FieldName)
	}

	relFields := make([]string, 0, len(t.Relationships))
	for _, rel := range t.Relationships {
		relFields = append(relFields, rel.FieldName)
	}

	ct := CachedTable{
		Name:                   t.TableName,
		Struct:                 t.StructName,
		Schema:                 schema,
		TableNameConstant:      t.TableNameConstant,
		FieldOptionsName:       t.StructName + "FieldOptions",
		CompositePK:            t.CompositePK,
		CompositePKStruct:      t.CompositePKStructName,
		PKColumns:              pkCols,
		ColumnFieldNames:       columnFields,
		RelationshipFieldNames: relFields,
		TTL:                    config.ResolveTableCacheTTL(tc, cfg.Cache),
	}
	if !t.CompositePK && len(pkCols) == 1 {
		ct.PKGoType = pkCols[0].GoType
	}
	if t.Tenancy != nil && t.Tenancy.Tenanted {
		ct.Tenanted = true
		ct.TenantGoType = t.Tenancy.GoType
		ct.TenantInPK = t.Tenancy.InPrimaryKey
		ct.TenantFieldName = t.Tenancy.FieldName
	}

	ct.Fingerprint = computeFingerprint(t, ct.Schema, cfg.Cache.Serializer, cfg.Cache.Version, ct.Tenanted)
	return ct
}

func buildCachedView(v ViewContext, vc config.ViewConfig, cfg *config.RootConfig) CachedView {
	schema := normalizeCacheSchema(v.Schema, cfg.Input.Dialect)

	invalidateOn := append([]string(nil), vc.InvalidateOn...)
	sort.Strings(invalidateOn)

	return CachedView{
		Name:              v.ViewName,
		Struct:            v.StructName,
		Schema:            schema,
		TableNameConstant: v.TableNameConstant,
		TTL:               config.ResolveViewCacheTTL(vc, cfg.Cache),
		InvalidateOn:      invalidateOn,
		Fingerprint:       computeViewFingerprint(v, schema, cfg.Cache.Serializer, cfg.Cache.Version),
	}
}

// normalizeCacheSchema matches the key-grammar rules in PRD §27.5: PostgreSQL
// empty schema → "public"; MySQL/SQLite → "".
func normalizeCacheSchema(schema string, dialect config.Dialect) string {
	if dialect == config.DialectPostgres {
		if schema == "" {
			return "public"
		}
		return schema
	}
	return ""
}

// fingerprintColumn is the per-column input to the fingerprint hash. Kept a
// separate type (rather than a map) so the JSON encoding is deterministic and
// column order is preserved.
type fingerprintColumn struct {
	Name      string `json:"name"`
	GoType    string `json:"go_type"`
	StructTag string `json:"struct_tag"`
}

// computeFingerprint implements CACHE.md §10.1 + PRD §29.5: sha256 over
// table ⊕ schema ⊕ json(columns) ⊕ json(pk columns) ⊕ serializer_id ⊕
// cache version ⊕ tenanted-marker. The tenancy structural marker (the
// boolean fact "is this table tenanted") changes the fingerprint when
// toggled — old non-tenant-keyed entries from a pre-tenancy era cannot
// satisfy a post-tenancy lookup. The tenant *value* is never folded in;
// it lives in the per-key tenant: segment instead.
func computeFingerprint(t TableContext, schema string, serializer config.Serializer, version int, tenanted bool) string {
	cols := make([]fingerprintColumn, 0, len(t.Columns))
	for _, c := range t.Columns {
		cols = append(cols, fingerprintColumn{
			Name:      c.Name,
			GoType:    c.GoType,
			StructTag: c.DBTag,
		})
	}

	pkNames := make([]string, 0, len(t.PKColumns))
	for _, c := range t.PKColumns {
		pkNames = append(pkNames, c.Name)
	}

	return fingerprintBytes(t.TableName, schema, cols, pkNames, serializer, version, tenanted)
}

func computeViewFingerprint(v ViewContext, schema string, serializer config.Serializer, version int) string {
	cols := make([]fingerprintColumn, 0, len(v.Columns))
	for _, c := range v.Columns {
		cols = append(cols, fingerprintColumn{
			Name:      c.Name,
			GoType:    c.GoType,
			StructTag: c.DBTag,
		})
	}

	pkNames := make([]string, 0, len(v.PKColumns))
	for _, c := range v.PKColumns {
		pkNames = append(pkNames, c.Name)
	}

	// Views are never tenanted at the cache layer (no tenant: segment is
	// emitted in their keys), so their fingerprint input never carries the
	// tenancy marker.
	return fingerprintBytes(v.ViewName, schema, cols, pkNames, serializer, version, false)
}

func fingerprintBytes(name, schema string, cols []fingerprintColumn, pkNames []string, serializer config.Serializer, version int, tenanted bool) string {
	h := sha256.New()
	h.Write([]byte(name))
	h.Write([]byte{0x00})
	h.Write([]byte(schema))
	h.Write([]byte{0x00})
	if data, err := json.Marshal(cols); err == nil {
		h.Write(data)
	}
	if data, err := json.Marshal(pkNames); err == nil {
		h.Write(data)
	}
	h.Write([]byte(serializerID(serializer)))
	_, _ = fmt.Fprintf(h, "%d", version)
	if tenanted {
		h.Write([]byte("|tenanted"))
	}

	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:4])
}

// serializerID produces the fingerprint-input identity for a serializer.
// For "custom" the generator cannot know the user's concrete type, so the
// identity is a stable literal; runtime overrides via WithSerializer do not
// participate in fingerprinting (CACHE.md §10.1).
func serializerID(s config.Serializer) string {
	switch s {
	case config.SerializerJSON:
		return "json"
	case config.SerializerMsgpack:
		return "msgpack"
	case config.SerializerCustom:
		return "custom"
	default:
		return string(s)
	}
}

// buildViewInvalidateMap inverts view → source tables into source table →
// view BuildTablePattern strings so the cache hook can fan out pattern
// invalidations by source table in O(1).
//
// The map is keyed by the source table's hook.TableName value, since the
// template emits each key as a `case hook.TableName(...)` that the mutation
// hook's m.Table must equal. An invalidate_on entry is a config spelling, not
// that value: `products` and `public.products` both name the table whose
// constant is "public.products" on PostgreSQL (PRD §5.5, §8.5), so each entry
// is resolved through tableNameValues. Keyed by the spelling, a qualified
// entry never matched the old bare values, and a bare entry would never match
// the qualified ones.
func buildViewInvalidateMap(views []CachedView, tables []TableContext, prefix string) map[string][]string {
	values := tableNameValues(tables)
	m := make(map[string][]string)
	for _, v := range views {
		pattern := buildTablePatternString(prefix, v.Schema, v.Name)
		for _, src := range v.InvalidateOn {
			for _, value := range values[src] {
				m[value] = append(m[value], pattern)
			}
		}
	}
	for k := range m {
		slices.Sort(m[k])
		m[k] = slices.Compact(m[k])
	}
	return m
}

// tableNameValues maps each config spelling of a generated table to the
// hook.TableName values it names. A qualified spelling names one table; a
// bare one names the table of that name in every schema (PRD §5.5 "Config key
// matching"). A spelling for a table that generated nothing — no primary key,
// say — has no entry: it has no constant and no mutation hook to match.
func tableNameValues(tables []TableContext) map[string][]string {
	values := make(map[string][]string, len(tables)*2)
	for _, tc := range tables {
		value := qualifiedTableName(tc.Schema, tc.TableName)
		values[tc.TableName] = append(values[tc.TableName], value)
		if value != tc.TableName {
			values[value] = append(values[value], value)
		}
	}
	return values
}

// buildTablePatternString mirrors cache.BuildTablePattern at codegen time.
// The runtime helper exists but the generator precomputes the string so the
// template can emit literals without a runtime import dependency in the
// view-pattern switch.
func buildTablePatternString(prefix, schema, table string) string {
	return prefix + ":" + qualifiedTableName(schema, table) + ":*"
}

// cachedTableTypeImports returns the imports for the consumer-facing Go types
// cache.go.tmpl spells in the signatures it generates for one cached table:
// the tenant key (TenantGoType) and the single-column PK (PKGoType). Both can
// be custom types supplied through overrides.types — a uuid.UUID tenant key is
// the shipped case — and neither reaches cache_gen.go through any other seed,
// since buildCacheImports is otherwise a fixed list.
//
// A composite PK contributes nothing: the template spells it as the generated
// <T>PK struct, which is local to the package.
func cachedTableTypeImports(t TableContext) []string {
	var out []string
	if t.Tenancy != nil && t.Tenancy.Tenanted && t.Tenancy.Import != "" {
		out = append(out, t.Tenancy.Import)
	}
	// Mirrors the PKGoType guard in buildCachedTable.
	if !t.CompositePK && len(t.PKColumns) == 1 && t.PKColumns[0].Import != "" {
		out = append(out, t.PKColumns[0].Import)
	}
	return out
}

func buildCacheImports(globalSerializer config.Serializer, typeImports []string) []string {
	imps := []string{
		"context",
		"fmt",
		"io",
		"sync",
		"time",
		"golang.org/x/sync/singleflight",
		"github.com/teandresmith/sqlgen/cache",
		"github.com/teandresmith/sqlgen/database",
		"github.com/teandresmith/sqlgen/hook",
	}
	if globalSerializer == config.SerializerMsgpack {
		imps = append(imps, "github.com/teandresmith/sqlgen/cache/msgpack")
	}
	return UniqueImports(append(imps, typeImports...))
}

func findViewConfig(cfg *config.RootConfig, name, schema string) config.ViewConfig {
	if schema != "" {
		if vc, ok := cfg.Views[qualifiedTableName(schema, name)]; ok {
			return vc
		}
	}
	if vc, ok := cfg.Views[name]; ok {
		return vc
	}
	return config.ViewConfig{}
}

func parseDurationOrDefault(raw string, def time.Duration) time.Duration {
	if raw == "" {
		return def
	}
	if d, err := time.ParseDuration(raw); err == nil {
		return d
	}
	return def
}
