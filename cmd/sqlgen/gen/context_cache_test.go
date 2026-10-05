package gen_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
)

// --- Fixtures ---

func cacheTestConfig(enabled bool) *config.RootConfig {
	cfg := &config.RootConfig{}
	cfg.Input.Dialect = config.DialectPostgres
	cfg.Tables = make(map[string]config.TableConfig)
	cfg.Views = make(map[string]config.ViewConfig)
	if !enabled {
		return cfg
	}
	cfg.Cache = &config.CacheConfig{
		Enabled:    true,
		TTL:        "1h",
		Serializer: config.SerializerJSON,
		KeyPrefix:  "sqlgen",
		Hydration:  &config.HydrationConfig{Enabled: true, Timeout: "30s"},
		CircuitBreaker: &config.CircuitBreakerConfig{
			Enabled:           true,
			FailureThreshold:  5,
			ProbeInterval:     "30s",
			HalfOpenMaxProbes: 1,
		},
	}
	return cfg
}

func cacheTestTable() gen.TableContext {
	return gen.TableContext{
		StructName:        "Product",
		TableName:         "products",
		TableNameConstant: "TableProducts",
		Schema:            "public",
		Columns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "uuid.UUID", DBTag: "id", JSONTag: "id", PrimaryKey: true},
			{Name: "name", FieldName: "Name", GoType: "string", DBTag: "name", JSONTag: "name"},
			{Name: "price", FieldName: "Price", GoType: "decimal.Decimal", DBTag: "price", JSONTag: "price"},
		},
		PKColumns: []gen.ColumnContext{
			{Name: "id", FieldName: "ID", GoType: "uuid.UUID", DBTag: "id", JSONTag: "id", PrimaryKey: true},
		},
	}
}

// cacheTestTenantedTable is cacheTestTable with a tenant column, for the
// tenanted arms of the cache template (PRD §29.5).
func cacheTestTenantedTable() gen.TableContext {
	t := cacheTestTable()
	t.StructName = "Article"
	t.TableName = "articles"
	t.TableNameConstant = "TableArticles"
	t.Columns = append(t.Columns, gen.ColumnContext{
		Name: "workspace_id", FieldName: "WorkspaceID", GoType: "uuid.UUID",
		DBTag: "workspace_id", JSONTag: "workspace_id",
	})
	t.Tenancy = &gen.TableTenancyContext{
		Tenanted:  true,
		Column:    "workspace_id",
		FieldName: "WorkspaceID",
		GoType:    "uuid.UUID",
		Required:  true,
	}
	return t
}

func cacheTestCompositeTable() gen.TableContext {
	return gen.TableContext{
		StructName:            "OrderItem",
		TableName:             "order_items",
		TableNameConstant:     "TableOrderItems",
		Schema:                "public",
		CompositePK:           true,
		CompositePKStructName: "OrderItemPK",
		Columns: []gen.ColumnContext{
			{Name: "order_id", FieldName: "OrderID", GoType: "uuid.UUID", DBTag: "order_id", JSONTag: "order_id", PrimaryKey: true},
			{Name: "product_id", FieldName: "ProductID", GoType: "uuid.UUID", DBTag: "product_id", JSONTag: "product_id", PrimaryKey: true},
			{Name: "quantity", FieldName: "Quantity", GoType: "int32", DBTag: "quantity", JSONTag: "quantity"},
		},
		PKColumns: []gen.ColumnContext{
			{Name: "order_id", FieldName: "OrderID", GoType: "uuid.UUID", DBTag: "order_id", JSONTag: "order_id", PrimaryKey: true},
			{Name: "product_id", FieldName: "ProductID", GoType: "uuid.UUID", DBTag: "product_id", JSONTag: "product_id", PrimaryKey: true},
		},
	}
}

// --- BuildCacheContext returns nil when cache disabled ---

func TestBuildCacheContext_disabled(t *testing.T) {
	ctx := gen.BuildCacheContext(nil, nil, cacheTestConfig(false), "db", "Client")
	if ctx != nil {
		t.Fatalf("BuildCacheContext() = %v, want nil when cache disabled", ctx)
	}
}

func TestBuildCacheContext_explicitlyDisabled(t *testing.T) {
	cfg := cacheTestConfig(true)
	cfg.Cache.Enabled = false
	ctx := gen.BuildCacheContext(nil, nil, cfg, "db", "Client")
	if ctx != nil {
		t.Fatalf("BuildCacheContext() = %v, want nil when enabled=false", ctx)
	}
}

// --- Fingerprint determinism ---

func TestFingerprint_deterministic(t *testing.T) {
	cfg := cacheTestConfig(true)
	tables := []gen.TableContext{cacheTestTable()}
	c1 := gen.BuildCacheContext(tables, nil, cfg, "db", "Client")
	c2 := gen.BuildCacheContext(tables, nil, cfg, "db", "Client")
	if c1 == nil || c2 == nil {
		t.Fatalf("expected non-nil contexts")
	}
	if c1.CachedTables[0].Fingerprint != c2.CachedTables[0].Fingerprint {
		t.Errorf("fingerprint non-deterministic: %q vs %q",
			c1.CachedTables[0].Fingerprint, c2.CachedTables[0].Fingerprint)
	}
	if got := c1.CachedTables[0].Fingerprint; len(got) != 8 {
		t.Errorf("fingerprint length = %d, want 8: %q", len(got), got)
	}
}

// --- Fingerprint sensitivity ---

func TestFingerprint_columnRename(t *testing.T) {
	cfg := cacheTestConfig(true)
	base := cacheTestTable()
	renamed := cacheTestTable()
	renamed.Columns[1].Name = "title"

	c1 := gen.BuildCacheContext([]gen.TableContext{base}, nil, cfg, "db", "Client")
	c2 := gen.BuildCacheContext([]gen.TableContext{renamed}, nil, cfg, "db", "Client")
	if c1.CachedTables[0].Fingerprint == c2.CachedTables[0].Fingerprint {
		t.Error("fingerprint unchanged after column rename")
	}
}

func TestFingerprint_goTypeChange(t *testing.T) {
	cfg := cacheTestConfig(true)
	base := cacheTestTable()
	changed := cacheTestTable()
	changed.Columns[1].GoType = "*string"

	c1 := gen.BuildCacheContext([]gen.TableContext{base}, nil, cfg, "db", "Client")
	c2 := gen.BuildCacheContext([]gen.TableContext{changed}, nil, cfg, "db", "Client")
	if c1.CachedTables[0].Fingerprint == c2.CachedTables[0].Fingerprint {
		t.Error("fingerprint unchanged after Go type change")
	}
}

func TestFingerprint_columnAdd(t *testing.T) {
	cfg := cacheTestConfig(true)
	base := cacheTestTable()
	extended := cacheTestTable()
	extended.Columns = append(extended.Columns, gen.ColumnContext{
		Name: "description", FieldName: "Description", GoType: "string", DBTag: "description", JSONTag: "description",
	})

	c1 := gen.BuildCacheContext([]gen.TableContext{base}, nil, cfg, "db", "Client")
	c2 := gen.BuildCacheContext([]gen.TableContext{extended}, nil, cfg, "db", "Client")
	if c1.CachedTables[0].Fingerprint == c2.CachedTables[0].Fingerprint {
		t.Error("fingerprint unchanged after column add")
	}
}

func TestFingerprint_serializerSwitch(t *testing.T) {
	base := cacheTestConfig(true)
	msgpack := cacheTestConfig(true)
	msgpack.Cache.Serializer = config.SerializerMsgpack

	tables := []gen.TableContext{cacheTestTable()}
	c1 := gen.BuildCacheContext(tables, nil, base, "db", "Client")
	c2 := gen.BuildCacheContext(tables, nil, msgpack, "db", "Client")
	if c1.CachedTables[0].Fingerprint == c2.CachedTables[0].Fingerprint {
		t.Error("fingerprint unchanged after serializer switch")
	}
}

func TestFingerprint_versionBump(t *testing.T) {
	base := cacheTestConfig(true)
	bumped := cacheTestConfig(true)
	bumped.Cache.Version = 2

	tables := []gen.TableContext{cacheTestTable()}
	c1 := gen.BuildCacheContext(tables, nil, base, "db", "Client")
	c2 := gen.BuildCacheContext(tables, nil, bumped, "db", "Client")
	if c1.CachedTables[0].Fingerprint == c2.CachedTables[0].Fingerprint {
		t.Error("fingerprint unchanged after cache.version bump")
	}
}

func TestFingerprint_pkOrderChange(t *testing.T) {
	cfg := cacheTestConfig(true)
	base := cacheTestCompositeTable()
	reordered := cacheTestCompositeTable()
	reordered.PKColumns[0], reordered.PKColumns[1] = reordered.PKColumns[1], reordered.PKColumns[0]

	c1 := gen.BuildCacheContext([]gen.TableContext{base}, nil, cfg, "db", "Client")
	c2 := gen.BuildCacheContext([]gen.TableContext{reordered}, nil, cfg, "db", "Client")
	if c1.CachedTables[0].Fingerprint == c2.CachedTables[0].Fingerprint {
		t.Error("fingerprint unchanged after PK column reorder")
	}
}

// --- Schema normalization ---

func TestBuildCacheContext_postgresEmptySchemaNormalized(t *testing.T) {
	cfg := cacheTestConfig(true)
	t1 := cacheTestTable()
	t1.Schema = ""
	ctx := gen.BuildCacheContext([]gen.TableContext{t1}, nil, cfg, "db", "Client")
	if got := ctx.CachedTables[0].Schema; got != "public" {
		t.Errorf("Schema = %q, want %q", got, "public")
	}
}

func TestBuildCacheContext_mysqlSchemaIsEmpty(t *testing.T) {
	cfg := cacheTestConfig(true)
	cfg.Input.Dialect = config.DialectMySQL
	t1 := cacheTestTable()
	t1.Schema = "something"
	ctx := gen.BuildCacheContext([]gen.TableContext{t1}, nil, cfg, "db", "Client")
	if got := ctx.CachedTables[0].Schema; got != "" {
		t.Errorf("Schema = %q, want empty for MySQL", got)
	}
}

// --- Per-table PK type resolution ---

func TestCachedTable_singlePKGoType(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	got := ctx.CachedTables[0]
	if got.CompositePK {
		t.Fatal("CompositePK = true, want false")
	}
	if got.PKGoType != "uuid.UUID" {
		t.Errorf("PKGoType = %q, want uuid.UUID", got.PKGoType)
	}
}

func TestCachedTable_compositePKStruct(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestCompositeTable()}, nil, cfg, "db", "Client")
	got := ctx.CachedTables[0]
	if !got.CompositePK {
		t.Fatal("CompositePK = false, want true")
	}
	if got.CompositePKStruct != "OrderItemPK" {
		t.Errorf("CompositePKStruct = %q, want OrderItemPK", got.CompositePKStruct)
	}
	if len(got.PKColumns) != 2 {
		t.Errorf("len(PKColumns) = %d, want 2", len(got.PKColumns))
	}
}

// --- View invalidate map ---

// TestBuildCacheContext_viewInvalidateMap pins the map's keys to the source
// tables' hook.TableName values, which is what the generated
// `case hook.TableName(...)` compares m.Table against — not to the
// invalidate_on spelling. A qualified spelling keyed verbatim never matched a
// bare constant, and a bare spelling keyed verbatim never matches a qualified
// one, so either way the view silently never invalidated.
func TestBuildCacheContext_viewInvalidateMap(t *testing.T) {
	table := func(name, schema string) gen.TableContext {
		tc := cacheTestTable()
		tc.TableName = name
		tc.Schema = schema
		return tc
	}
	tests := []struct {
		name         string
		dialect      config.Dialect
		viewSchema   string
		tables       []gen.TableContext
		invalidateOn []string
		want         map[string][]string
	}{
		{
			name:         "postgres bare spelling resolves to the qualified value",
			dialect:      config.DialectPostgres,
			viewSchema:   "public",
			tables:       []gen.TableContext{table("products", "public"), table("reviews", "public")},
			invalidateOn: []string{"products", "reviews"},
			want: map[string][]string{
				"public.products": {"sqlgen:public.product_summary:*"},
				"public.reviews":  {"sqlgen:public.product_summary:*"},
			},
		},
		{
			name:         "postgres qualified spelling resolves to the same value",
			dialect:      config.DialectPostgres,
			viewSchema:   "public",
			tables:       []gen.TableContext{table("products", "public"), table("reviews", "public")},
			invalidateOn: []string{"public.products"},
			want: map[string][]string{
				"public.products": {"sqlgen:public.product_summary:*"},
			},
		},
		{
			name:         "postgres bare spelling names the table in every schema",
			dialect:      config.DialectPostgres,
			viewSchema:   "public",
			tables:       []gen.TableContext{table("products", "public"), table("products", "audit")},
			invalidateOn: []string{"products"},
			want: map[string][]string{
				"audit.products":  {"sqlgen:public.product_summary:*"},
				"public.products": {"sqlgen:public.product_summary:*"},
			},
		},
		{
			name:         "mysql has no schema, so the value is the bare name",
			dialect:      config.DialectMySQL,
			viewSchema:   "",
			tables:       []gen.TableContext{table("products", ""), table("reviews", "")},
			invalidateOn: []string{"products"},
			want: map[string][]string{
				"products": {"sqlgen:product_summary:*"},
			},
		},
		{
			name:         "a source that generated nothing has no key",
			dialect:      config.DialectPostgres,
			viewSchema:   "public",
			tables:       []gen.TableContext{table("products", "public")},
			invalidateOn: []string{"pkless"},
			want:         map[string][]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := cacheTestConfig(true)
			cfg.Input.Dialect = tt.dialect
			cfg.Views["product_summary"] = config.ViewConfig{
				StructName:   "ProductSummary",
				Cache:        &config.ViewCacheConfig{Enabled: new(true)},
				InvalidateOn: tt.invalidateOn,
			}
			views := []gen.ViewContext{{
				StructName:        "ProductSummary",
				ViewName:          "product_summary",
				TableNameConstant: "TableProductSummaries",
				Schema:            tt.viewSchema,
				Columns: []gen.ColumnContext{
					{Name: "product_id", FieldName: "ProductID", GoType: "uuid.UUID", DBTag: "product_id", JSONTag: "product_id"},
				},
			}}
			ctx := gen.BuildCacheContext(tt.tables, views, cfg, "db", "Client")
			if diff := cmp.Diff(tt.want, ctx.ViewInvalidateMap); diff != "" {
				t.Errorf("ViewInvalidateMap mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// --- Imports ---

func TestBuildCacheContext_jsonImports(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	for _, imp := range ctx.Imports {
		if strings.Contains(imp, "cache/msgpack") {
			t.Errorf("json config emitted msgpack import: %q", imp)
		}
	}
	if !hasImport(ctx.Imports, "github.com/teandresmith/sqlgen/cache") {
		t.Errorf("missing runtime cache import")
	}
	if !hasImport(ctx.Imports, "golang.org/x/sync/singleflight") {
		t.Errorf("missing singleflight import")
	}
}

func TestBuildCacheContext_msgpackImports(t *testing.T) {
	cfg := cacheTestConfig(true)
	cfg.Cache.Serializer = config.SerializerMsgpack
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	if !hasImport(ctx.Imports, "github.com/teandresmith/sqlgen/cache/msgpack") {
		t.Errorf("missing msgpack import when serializer=msgpack, got imports: %v", ctx.Imports)
	}
}

// --- TTL resolution ---

func TestBuildCacheContext_ttlResolution(t *testing.T) {
	cfg := cacheTestConfig(true)
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	if got := ctx.CachedTables[0].TTL; got != time.Hour {
		t.Errorf("TTL = %v, want 1h", got)
	}
	if got := ctx.Config.DefaultTTL; got != time.Hour {
		t.Errorf("DefaultTTL = %v, want 1h", got)
	}
}

// --- Serializer ID propagation for template use ---

func TestBuildCacheContext_serializerID(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   config.Serializer
		want string
	}{
		{"json", config.SerializerJSON, "json"},
		{"msgpack", config.SerializerMsgpack, "msgpack"},
		{"custom", config.SerializerCustom, "custom"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := cacheTestConfig(true)
			cfg.Cache.Serializer = tc.in
			ctx := gen.BuildCacheContext(nil, nil, cfg, "db", "Client")
			if got := ctx.Config.SerializerID; got != tc.want {
				t.Errorf("SerializerID = %q, want %q", got, tc.want)
			}
		})
	}
}

// --- Per-table enabled resolution ---

func TestBuildCacheContext_tableOptOut(t *testing.T) {
	cfg := cacheTestConfig(true)
	cfg.Tables["products"] = config.TableConfig{
		Cache: &config.TableCacheConfig{
			Enabled: new(false),
		},
	}
	ctx := gen.BuildCacheContext([]gen.TableContext{cacheTestTable()}, nil, cfg, "db", "Client")
	if len(ctx.CachedTables) != 0 {
		t.Errorf("CachedTables has %d entries, want 0 when table opts out", len(ctx.CachedTables))
	}
}

// --- Tenancy structural marker (PRD §29.5) ---

func TestFingerprint_tenancyStructuralMarker_changesFingerprint(t *testing.T) {
	cfg := cacheTestConfig(true)

	plain := cacheTestTable()
	tenanted := cacheTestTable()
	tenanted.Tenancy = &gen.TableTenancyContext{
		Tenanted:  true,
		Column:    "workspace_id",
		FieldName: "WorkspaceID",
		GoType:    "uuid.UUID",
		Import:    "github.com/google/uuid",
	}

	c1 := gen.BuildCacheContext([]gen.TableContext{plain}, nil, cfg, "db", "Client")
	c2 := gen.BuildCacheContext([]gen.TableContext{tenanted}, nil, cfg, "db", "Client")
	if c1.CachedTables[0].Fingerprint == c2.CachedTables[0].Fingerprint {
		t.Errorf("fingerprint unchanged after toggling tenancy structural marker: %q", c1.CachedTables[0].Fingerprint)
	}

	// Sanity: the tenanted CachedTable carries the structural flag through.
	if !c2.CachedTables[0].Tenanted {
		t.Errorf("CachedTable.Tenanted = false, want true")
	}
	if c2.CachedTables[0].TenantGoType != "uuid.UUID" {
		t.Errorf("CachedTable.TenantGoType = %q, want %q", c2.CachedTables[0].TenantGoType, "uuid.UUID")
	}
}

func TestFingerprint_tenantValueDoesNotEnter(t *testing.T) {
	// The tenant *value* lives in the per-key tenant: segment, never in
	// the fingerprint. There is no way to vary the tenant value from
	// codegen-side fixtures since the fingerprint inputs do not include
	// it; this test asserts that two tenanted tables with identical
	// schema produce identical fingerprints regardless of tenant column
	// import / type aliasing on the (Go-side) tenant identity.
	cfg := cacheTestConfig(true)

	a := cacheTestTable()
	a.Tenancy = &gen.TableTenancyContext{
		Tenanted: true, Column: "workspace_id", FieldName: "WorkspaceID",
		GoType: "uuid.UUID", Import: "github.com/google/uuid",
	}
	b := cacheTestTable()
	b.Tenancy = &gen.TableTenancyContext{
		Tenanted: true, Column: "workspace_id", FieldName: "WorkspaceID",
		GoType: "uuid.UUID", Import: "github.com/google/uuid",
	}

	c1 := gen.BuildCacheContext([]gen.TableContext{a}, nil, cfg, "db", "Client")
	c2 := gen.BuildCacheContext([]gen.TableContext{b}, nil, cfg, "db", "Client")
	if c1.CachedTables[0].Fingerprint != c2.CachedTables[0].Fingerprint {
		t.Errorf("fingerprints differ for two tenanted tables with identical schema: %q vs %q",
			c1.CachedTables[0].Fingerprint, c2.CachedTables[0].Fingerprint)
	}
}

// --- helpers ---

func hasImport(imports []string, want string) bool {
	return slices.Contains(imports, want)
}
