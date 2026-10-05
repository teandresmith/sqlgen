package config_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// writeTestConfig writes a YAML config file to a temp directory and returns the file path.
func writeTestConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sqlgen.yml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	return path
}

func TestLoadConfig_MinimalWithDefaults(t *testing.T) {
	path := writeTestConfig(t, `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./internal/models
`)

	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}

	// Version default.
	if cfg.Version != "v1" {
		t.Errorf("Version = %q, want %q", cfg.Version, "v1")
	}

	// Input defaults.
	if cfg.Input.Dialect != "postgres" {
		t.Errorf("Input.Dialect = %q, want %q", cfg.Input.Dialect, "postgres")
	}
	if cfg.Input.Source != "files" {
		t.Errorf("Input.Source = %q, want %q", cfg.Input.Source, "files")
	}
	if cfg.Input.Schema != "*" {
		t.Errorf("Input.Schema = %q, want %q", cfg.Input.Schema, "*")
	}
	if diff := cmp.Diff([]string{"./migrations"}, cfg.Input.Paths); diff != "" {
		t.Errorf("Input.Paths mismatch (-want +got):\n%s", diff)
	}
	if cfg.Input.ParseMode != "strict" {
		t.Errorf("Input.ParseMode = %q, want %q", cfg.Input.ParseMode, "strict")
	}

	// Output defaults.
	if cfg.Output.Driver != "pgx" {
		t.Errorf("Output.Driver = %q, want %q", cfg.Output.Driver, "pgx")
	}
	if cfg.Output.Dir != "./internal/models" {
		t.Errorf("Output.Dir = %q, want %q", cfg.Output.Dir, "./internal/models")
	}
	if cfg.Output.Package != "models" {
		t.Errorf("Output.Package = %q, want %q", cfg.Output.Package, "models")
	}
	if cfg.Output.Layout != "single_file" {
		t.Errorf("Output.Layout = %q, want %q", cfg.Output.Layout, "single_file")
	}

	// Enum output defaults.
	if cfg.Output.Enums.File != "enums_gen.go" {
		t.Errorf("Output.Enums.File = %q, want %q", cfg.Output.Enums.File, "enums_gen.go")
	}
	if cfg.Output.Enums.Package != "models" {
		t.Errorf("Output.Enums.Package = %q, want %q", cfg.Output.Enums.Package, "models")
	}

	// Type output defaults.
	if cfg.Output.Types.File != "types_gen.go" {
		t.Errorf("Output.Types.File = %q, want %q", cfg.Output.Types.File, "types_gen.go")
	}
	if cfg.Output.Types.Package != "models" {
		t.Errorf("Output.Types.Package = %q, want %q", cfg.Output.Types.Package, "models")
	}

	// Client output defaults.
	if cfg.Output.Client.Name != "Client" {
		t.Errorf("Output.Client.Name = %q, want %q", cfg.Output.Client.Name, "Client")
	}
	if cfg.Output.Client.File != "client_gen.go" {
		t.Errorf("Output.Client.File = %q, want %q", cfg.Output.Client.File, "client_gen.go")
	}

	// Generation defaults.
	if *cfg.Generation.QueryLimit != 1000 {
		t.Errorf("Generation.QueryLimit = %d, want %d", *cfg.Generation.QueryLimit, 1000)
	}
	if *cfg.Generation.BatchSize != 200 {
		t.Errorf("Generation.BatchSize = %d, want %d", *cfg.Generation.BatchSize, 200)
	}
	if *cfg.Generation.PageSize != 100 {
		t.Errorf("Generation.PageSize = %d, want %d", *cfg.Generation.PageSize, 100)
	}
	if diff := cmp.Diff([]string{"id"}, cfg.Generation.CursorKeys); diff != "" {
		t.Errorf("Generation.CursorKeys mismatch (-want +got):\n%s", diff)
	}
	if cfg.Generation.UUIDVersion != "v4" {
		t.Errorf("Generation.UUIDVersion = %q, want %q", cfg.Generation.UUIDVersion, "v4")
	}
	if *cfg.Generation.StrictUpdates != true {
		t.Errorf("Generation.StrictUpdates = %v, want true", *cfg.Generation.StrictUpdates)
	}

	// Overrides defaults.
	if *cfg.Overrides.UsePointers != true {
		t.Errorf("Overrides.UsePointers = %v, want true", *cfg.Overrides.UsePointers)
	}

	// Maps initialized.
	if cfg.Tables == nil {
		t.Error("Tables map should be initialized, got nil")
	}
	if cfg.Views == nil {
		t.Error("Views map should be initialized, got nil")
	}
	if cfg.Extras == nil {
		t.Error("Extras map should be initialized, got nil")
	}
}

func TestLoadConfig_DefaultSoftDeleteColumns(t *testing.T) {
	path := writeTestConfig(t, `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models
`)

	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}

	want := []config.SoftDeleteConfig{
		{Name: "deleted_at", Type: "timestamp"},
		{Name: "deleted_datetime", Type: "timestamp"},
		{Name: "is_deleted", Type: "bool"},
		{Name: "deleted_flag", Type: "bool"},
		{Name: "deleted", Type: "bool"},
	}
	if diff := cmp.Diff(want, cfg.Generation.SoftDeleteColumns); diff != "" {
		t.Errorf("Generation.SoftDeleteColumns mismatch (-want +got):\n%s", diff)
	}
}

func TestLoadConfig_DefaultUpdateColumns(t *testing.T) {
	path := writeTestConfig(t, `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models
`)

	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}

	want := []string{"updated_at", "updated_datetime", "last_modified_at", "modified_on"}
	if diff := cmp.Diff(want, cfg.Generation.UpdateColumns); diff != "" {
		t.Errorf("Generation.UpdateColumns mismatch (-want +got):\n%s", diff)
	}
}

func TestLoadConfig_FullConfig(t *testing.T) {
	path := writeTestConfig(t, `
version: "v1"
input:
  dialect: postgres
  source: files
  schema: "public"
  paths:
    - "./migrations"
  parse_mode: strict
  connection:
    url: "postgres://localhost/mydb"
    host: localhost
    port: 5432
    user: admin
    password: secret
    database: mydb
    ssl_mode: disable
  introspect:
    schemas: [public, private]
    exclude_schemas: [pg_catalog]
    tables: [users, products]

output:
  driver: pgx
  dir: ./internal/models
  package: models
  layout: file_per_table
  enums:
    file: enums_gen.go
    package: models
  types:
    file: types_gen.go
    package: models
  client:
    name: Client
    file: client_gen.go

generation:
  query_limit: 500
  batch_size: 100
  page_size: 50
  cursor_keys: [id, created_at]
  uuid_version: v7
  strict_updates: false
  soft_delete_columns:
    - name: deleted_at
      type: timestamp
    - name: is_deleted
      type: bool
  update_columns:
    - updated_at
    - last_modified_at
  exclude_columns:
    - _sync_version

api:
  enabled: true
  operations: no_hard_delete

overrides:
  use_pointers: false
  types:
    uuid:
      type: uuid.UUID
      import: github.com/google/uuid
      nullable: uuid.NullUUID
    numeric:
      type: decimal.Decimal
      import: github.com/shopspring/decimal
      nullable:
        type: decimal.NullDecimal
        import: github.com/shopspring/decimal

tables:
  products:
    struct_name: Product
    description: "A product in the catalog"
    cursor_keys: [id, created_at]
    query_limit: 200
    batch_size: 50
    page_size: 25
    strict_updates: true
    exclude_columns:
      - internal_notes
    api:
      operations:
        preset: all
        update: false
        hard_delete: false
    primary_key:
      strategy: app
      uuid_version: v7
    column_map:
      sku_code:
        name: SKUCode
        description: "Stock keeping unit"
        type: string
        import: ""
    type_map:
      metadata: json.RawMessage
    relationships:
      - name: Tags
        type: m2m
        table: tags
        junction: product_tags
      - name: Comments
        type: o2m
        table: comments
        fk: commentable_id
        filter: "commentable_type = 'products'"
        sort:
          - column: created_at
            direction: desc
    overrides:
      use_pointers: true
      types:
        jsonb:
          type: json.RawMessage
          import: encoding/json

  audit_logs:
    api:
      operations: append_only
    query_limit: 500

views:
  product_summary:
    struct_name: ProductSummary
    sql: "./views/product_summary.sql"

extras:
  MemeMetadata:
    description: "Metadata for meme entities"
    fields:
      ViewCount:
        type: int
        tags:
          json: view_count
      IsTopTen:
        type: bool
        tags:
          json: is_top_ten
`)

	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}

	// Input.
	if cfg.Input.Dialect != "postgres" {
		t.Errorf("Input.Dialect = %q, want %q", cfg.Input.Dialect, "postgres")
	}
	if cfg.Input.Source != "files" {
		t.Errorf("Input.Source = %q, want %q", cfg.Input.Source, "files")
	}
	if cfg.Input.Schema != "public" {
		t.Errorf("Input.Schema = %q, want %q", cfg.Input.Schema, "public")
	}
	if cfg.Input.Connection == nil {
		t.Fatal("Input.Connection should not be nil")
	}
	if cfg.Input.Connection.URL != "postgres://localhost/mydb" {
		t.Errorf("Connection.URL = %q, want %q", cfg.Input.Connection.URL, "postgres://localhost/mydb")
	}
	if cfg.Input.Connection.Port != 5432 {
		t.Errorf("Connection.Port = %d, want %d", cfg.Input.Connection.Port, 5432)
	}
	if cfg.Input.Introspect == nil {
		t.Fatal("Input.Introspect should not be nil")
	}
	if diff := cmp.Diff([]string{"public", "private"}, cfg.Input.Introspect.Schemas); diff != "" {
		t.Errorf("Introspect.Schemas mismatch (-want +got):\n%s", diff)
	}

	// Output.
	if cfg.Output.Layout != "file_per_table" {
		t.Errorf("Output.Layout = %q, want %q", cfg.Output.Layout, "file_per_table")
	}
	// Generation.
	if *cfg.Generation.QueryLimit != 500 {
		t.Errorf("Generation.QueryLimit = %d, want %d", *cfg.Generation.QueryLimit, 500)
	}
	if *cfg.Generation.BatchSize != 100 {
		t.Errorf("Generation.BatchSize = %d, want %d", *cfg.Generation.BatchSize, 100)
	}
	if *cfg.Generation.PageSize != 50 {
		t.Errorf("Generation.PageSize = %d, want %d", *cfg.Generation.PageSize, 50)
	}
	if cfg.Generation.UUIDVersion != "v7" {
		t.Errorf("Generation.UUIDVersion = %q, want %q", cfg.Generation.UUIDVersion, "v7")
	}
	if *cfg.Generation.StrictUpdates != false {
		t.Errorf("Generation.StrictUpdates = %v, want false", *cfg.Generation.StrictUpdates)
	}
	if cfg.API == nil || cfg.API.Operations == nil || cfg.API.Operations.Preset != "no_hard_delete" {
		t.Errorf("API.Operations = %+v, want preset %q", cfg.API, "no_hard_delete")
	}

	// Overrides.
	if *cfg.Overrides.UsePointers != false {
		t.Errorf("Overrides.UsePointers = %v, want false", *cfg.Overrides.UsePointers)
	}
	uuidOverride, ok := cfg.Overrides.Types["uuid"]
	if !ok {
		t.Fatal("Overrides.Types should contain 'uuid'")
	}
	if uuidOverride.Type != "uuid.UUID" {
		t.Errorf("uuid override Type = %q, want %q", uuidOverride.Type, "uuid.UUID")
	}
	if uuidOverride.Nullable.Type != "uuid.NullUUID" {
		t.Errorf("uuid override Nullable.Type = %q, want %q", uuidOverride.Nullable.Type, "uuid.NullUUID")
	}

	// Nullable with full form (numeric).
	numOverride, ok := cfg.Overrides.Types["numeric"]
	if !ok {
		t.Fatal("Overrides.Types should contain 'numeric'")
	}
	if numOverride.Nullable.Type != "decimal.NullDecimal" {
		t.Errorf("numeric override Nullable.Type = %q, want %q", numOverride.Nullable.Type, "decimal.NullDecimal")
	}
	if numOverride.Nullable.Import != "github.com/shopspring/decimal" {
		t.Errorf("numeric override Nullable.Import = %q, want %q", numOverride.Nullable.Import, "github.com/shopspring/decimal")
	}

	// Table config — products.
	products, ok := cfg.Tables["products"]
	if !ok {
		t.Fatal("Tables should contain 'products'")
	}
	if products.StructName != "Product" {
		t.Errorf("products.StructName = %q, want %q", products.StructName, "Product")
	}
	if products.Description != "A product in the catalog" {
		t.Errorf("products.Description = %q, want %q", products.Description, "A product in the catalog")
	}
	if *products.QueryLimit != 200 {
		t.Errorf("products.QueryLimit = %d, want %d", *products.QueryLimit, 200)
	}
	if products.PrimaryKey == nil {
		t.Fatal("products.PrimaryKey should not be nil")
	}
	if products.PrimaryKey.Strategy != "app" {
		t.Errorf("products.PrimaryKey.Strategy = %q, want %q", products.PrimaryKey.Strategy, "app")
	}
	if products.PrimaryKey.UUIDVersion != "v7" {
		t.Errorf("products.PrimaryKey.UUIDVersion = %q, want %q", products.PrimaryKey.UUIDVersion, "v7")
	}

	// Column map.
	skuOverride, ok := products.ColumnMap["sku_code"]
	if !ok {
		t.Fatal("products.ColumnMap should contain 'sku_code'")
	}
	if skuOverride.Name != "SKUCode" {
		t.Errorf("sku_code.Name = %q, want %q", skuOverride.Name, "SKUCode")
	}

	// Type map.
	if products.TypeMap["metadata"] != "json.RawMessage" {
		t.Errorf("products.TypeMap[metadata] = %q, want %q", products.TypeMap["metadata"], "json.RawMessage")
	}

	// Relationships.
	if len(products.Relationships) != 2 {
		t.Fatalf("products.Relationships length = %d, want 2", len(products.Relationships))
	}
	tags := products.Relationships[0]
	if tags.Name != "Tags" || tags.Type != "m2m" || tags.Junction != "product_tags" {
		t.Errorf("Tags relationship = {%q, %q, %q}, want {Tags, m2m, product_tags}", tags.Name, tags.Type, tags.Junction)
	}
	comments := products.Relationships[1]
	if comments.Filter != "commentable_type = 'products'" {
		t.Errorf("Comments.Filter = %q, want %q", comments.Filter, "commentable_type = 'products'")
	}
	if len(comments.Sort) != 1 || comments.Sort[0].Column != "created_at" || comments.Sort[0].Direction != "desc" {
		t.Errorf("Comments.Sort = %v, want [{created_at desc}]", comments.Sort)
	}

	// Table-level overrides.
	if products.Overrides == nil {
		t.Fatal("products.Overrides should not be nil")
	}
	if *products.Overrides.UsePointers != true {
		t.Errorf("products.Overrides.UsePointers = %v, want true", *products.Overrides.UsePointers)
	}

	// API mask with preset + keys.
	if products.API == nil || products.API.Operations == nil {
		t.Fatal("products.API.Operations should not be nil")
	}
	pOps := products.API.Operations
	if pOps.Preset != "all" {
		t.Errorf("products.API.Operations.Preset = %q, want %q", pOps.Preset, "all")
	}
	if pOps.Update == nil || *pOps.Update != false {
		t.Error("products.API.Operations.Update should be false")
	}
	if pOps.HardDelete == nil || *pOps.HardDelete != false {
		t.Error("products.API.Operations.HardDelete should be false")
	}

	// audit_logs — API mask as a bare preset string.
	auditLogs, ok := cfg.Tables["audit_logs"]
	if !ok {
		t.Fatal("Tables should contain 'audit_logs'")
	}
	if auditLogs.API == nil || auditLogs.API.Operations == nil {
		t.Fatal("audit_logs.API.Operations should not be nil")
	}
	if auditLogs.API.Operations.Preset != "append_only" {
		t.Errorf("audit_logs.API.Operations.Preset = %q, want %q", auditLogs.API.Operations.Preset, "append_only")
	}

	// Views.
	view, ok := cfg.Views["product_summary"]
	if !ok {
		t.Fatal("Views should contain 'product_summary'")
	}
	if view.StructName != "ProductSummary" {
		t.Errorf("product_summary.StructName = %q, want %q", view.StructName, "ProductSummary")
	}
	if view.SQL != "./views/product_summary.sql" {
		t.Errorf("product_summary.SQL = %q, want %q", view.SQL, "./views/product_summary.sql")
	}

	// Extras.
	extra, ok := cfg.Extras["MemeMetadata"]
	if !ok {
		t.Fatal("Extras should contain 'MemeMetadata'")
	}
	if extra.Description != "Metadata for meme entities" {
		t.Errorf("MemeMetadata.Description = %q, want %q", extra.Description, "Metadata for meme entities")
	}
	vcField, ok := extra.Fields["ViewCount"]
	if !ok {
		t.Fatal("MemeMetadata.Fields should contain 'ViewCount'")
	}
	if vcField.Type != "int" {
		t.Errorf("ViewCount.Type = %q, want %q", vcField.Type, "int")
	}
	if vcField.Tags["json"] != "view_count" {
		t.Errorf("ViewCount.Tags[json] = %q, want %q", vcField.Tags["json"], "view_count")
	}
}

func TestLoadConfig_EnvVarSQLGEN_DB_URL(t *testing.T) {
	path := writeTestConfig(t, `
input:
  dialect: postgres
  paths: ["./migrations"]
  connection:
    url: "postgres://original/db"
    host: original-host
    port: 5432
    user: original-user
    password: original-pass
    database: original-db
output:
  dir: ./models
`)

	t.Setenv("SQLGEN_DB_URL", "postgres://env-override/db")

	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}

	if cfg.Input.Connection.URL != "postgres://env-override/db" {
		t.Errorf("Connection.URL = %q, want %q", cfg.Input.Connection.URL, "postgres://env-override/db")
	}
	// Individual fields should be cleared when URL is set from env.
	if cfg.Input.Connection.Host != "" {
		t.Errorf("Connection.Host = %q, want empty (cleared by SQLGEN_DB_URL)", cfg.Input.Connection.Host)
	}
	if cfg.Input.Connection.Port != 0 {
		t.Errorf("Connection.Port = %d, want 0 (cleared by SQLGEN_DB_URL)", cfg.Input.Connection.Port)
	}
	if cfg.Input.Connection.User != "" {
		t.Errorf("Connection.User = %q, want empty (cleared by SQLGEN_DB_URL)", cfg.Input.Connection.User)
	}
	if cfg.Input.Connection.Password != "" {
		t.Errorf("Connection.Password = %q, want empty (cleared by SQLGEN_DB_URL)", cfg.Input.Connection.Password)
	}
	if cfg.Input.Connection.Database != "" {
		t.Errorf("Connection.Database = %q, want empty (cleared by SQLGEN_DB_URL)", cfg.Input.Connection.Database)
	}
}

func TestLoadConfig_EnvVarSQLGEN_DB_PASSWORD(t *testing.T) {
	path := writeTestConfig(t, `
input:
  dialect: postgres
  paths: ["./migrations"]
  connection:
    host: localhost
    port: 5432
    user: admin
    password: config-password
    database: mydb
output:
  dir: ./models
`)

	t.Setenv("SQLGEN_DB_PASSWORD", "env-secret-password")

	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}

	if cfg.Input.Connection.Password != "env-secret-password" {
		t.Errorf("Connection.Password = %q, want %q", cfg.Input.Connection.Password, "env-secret-password")
	}
	// Other fields should be preserved.
	if cfg.Input.Connection.Host != "localhost" {
		t.Errorf("Connection.Host = %q, want %q", cfg.Input.Connection.Host, "localhost")
	}
	if cfg.Input.Connection.User != "admin" {
		t.Errorf("Connection.User = %q, want %q", cfg.Input.Connection.User, "admin")
	}
}

func TestLoadConfig_EnvVarSQLGEN_DB_URL_NoConnection(t *testing.T) {
	path := writeTestConfig(t, `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models
`)

	t.Setenv("SQLGEN_DB_URL", "postgres://env/db")

	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}

	if cfg.Input.Connection == nil {
		t.Fatal("Connection should be created when SQLGEN_DB_URL is set")
	}
	if cfg.Input.Connection.URL != "postgres://env/db" {
		t.Errorf("Connection.URL = %q, want %q", cfg.Input.Connection.URL, "postgres://env/db")
	}
}

func TestLoadConfig_OutputPackageDerivedFromDir(t *testing.T) {
	tests := []struct {
		name    string
		dir     string
		wantPkg string
	}{
		{
			name:    "simple directory",
			dir:     "./models",
			wantPkg: "models",
		},
		{
			name:    "nested directory",
			dir:     "./internal/models",
			wantPkg: "models",
		},
		{
			name:    "hyphenated directory",
			dir:     "./my-models",
			wantPkg: "my_models",
		},
		{
			name:    "current directory",
			dir:     ".",
			wantPkg: ".",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTestConfig(t, `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: `+tt.dir+`
`)

			cfg, err := config.LoadConfig(path)
			if err != nil {
				t.Fatalf("LoadConfig() unexpected error: %v", err)
			}
			if cfg.Output.Package != tt.wantPkg {
				t.Errorf("Output.Package = %q, want %q", cfg.Output.Package, tt.wantPkg)
			}
		})
	}
}

func TestPerTableOverrideResolution(t *testing.T) {
	path := writeTestConfig(t, `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models

generation:
  query_limit: 500
  batch_size: 100
  page_size: 50
  cursor_keys: [id, created_at]
  strict_updates: false

tables:
  products:
    query_limit: 200
    batch_size: 50
    cursor_keys: [id]
    strict_updates: true
  orders: {}
`)

	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}

	products := cfg.Tables["products"]
	orders := cfg.Tables["orders"]

	// products: table-level overrides should take precedence.
	if got := config.ResolveTableQueryLimit(products, cfg.Generation); got != 200 {
		t.Errorf("ResolveTableQueryLimit(products) = %d, want 200", got)
	}
	if got := config.ResolveTableBatchSize(products, cfg.Generation); got != 50 {
		t.Errorf("ResolveTableBatchSize(products) = %d, want 50", got)
	}
	if got := config.ResolveTablePageSize(products, cfg.Generation); got != 50 {
		t.Errorf("ResolveTablePageSize(products) = %d, want 50 (falls through to global)", got)
	}
	productsDecision := config.ResolveTableConnection(products, cfg.Generation, "public.products", []string{"id", "name"}, []string{"id"})
	if diff := cmp.Diff([]string{"id"}, productsDecision.Keys); diff != "" {
		t.Errorf("ResolveTableConnection(products) keys mismatch (-want +got):\n%s", diff)
	}
	if !productsDecision.Emit {
		t.Errorf("ResolveTableConnection(products) Emit = false, want true")
	}
	if got := config.ResolveTableStrictUpdates(products, cfg.Generation); got != true {
		t.Errorf("ResolveTableStrictUpdates(products) = %v, want true", got)
	}

	// orders: no table-level overrides, should fall through to global.
	if got := config.ResolveTableQueryLimit(orders, cfg.Generation); got != 500 {
		t.Errorf("ResolveTableQueryLimit(orders) = %d, want 500", got)
	}
	if got := config.ResolveTableBatchSize(orders, cfg.Generation); got != 100 {
		t.Errorf("ResolveTableBatchSize(orders) = %d, want 100", got)
	}
	if got := config.ResolveTablePageSize(orders, cfg.Generation); got != 50 {
		t.Errorf("ResolveTablePageSize(orders) = %d, want 50", got)
	}
	ordersDecision := config.ResolveTableConnection(orders, cfg.Generation, "public.orders", []string{"id", "created_at", "user_id"}, []string{"id"})
	if diff := cmp.Diff([]string{"id", "created_at"}, ordersDecision.Keys); diff != "" {
		t.Errorf("ResolveTableConnection(orders) keys mismatch (-want +got):\n%s", diff)
	}
	if !ordersDecision.Emit {
		t.Errorf("ResolveTableConnection(orders) Emit = false, want true")
	}
	if got := config.ResolveTableStrictUpdates(orders, cfg.Generation); got != false {
		t.Errorf("ResolveTableStrictUpdates(orders) = %v, want false", got)
	}
}

func TestPerTableExcludeColumnsMerge(t *testing.T) {
	path := writeTestConfig(t, `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models

generation:
  exclude_columns:
    - _sync_version
    - internal_notes

tables:
  products:
    exclude_columns:
      - secret_key
      - _sync_version
`)

	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}

	products := cfg.Tables["products"]
	got := config.ResolveTableExcludeColumns(products, cfg.Generation)

	// Should be union: _sync_version, internal_notes, secret_key (deduped).
	want := []string{"_sync_version", "internal_notes", "secret_key"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ResolveTableExcludeColumns() mismatch (-want +got):\n%s", diff)
	}
}

// nestedToggles is the four-field view every ExpandPreset arm must resolve.
// A preset is an explicit enumeration, not a partial overlay (PRD §4.6): a
// field an arm does not name stays nil, and every consumer dereferences, so
// the symptom of a forgotten arm is a nil-pointer panic rather than a wrong
// default. assertNestedToggles reports the nil before the dereference can.
type nestedToggles struct {
	upsertMany, createWithRelated, updateWithRelated, upsertWithRelated bool
}

func assertNestedToggles(t *testing.T, preset string, ops config.Operations, want nestedToggles) {
	t.Helper()
	for _, tt := range []struct {
		key  string
		got  *bool
		want bool
	}{
		{"upsert_many", ops.UpsertMany, want.upsertMany},
		{"create_with_related", ops.CreateWithRelated, want.createWithRelated},
		{"update_with_related", ops.UpdateWithRelated, want.updateWithRelated},
		{"upsert_with_related", ops.UpsertWithRelated, want.upsertWithRelated},
	} {
		if tt.got == nil {
			t.Errorf("ExpandPreset(%q).%s = nil, want %v — the arm does not resolve it", preset, tt.key, tt.want)
			continue
		}
		if *tt.got != tt.want {
			t.Errorf("ExpandPreset(%q).%s = %v, want %v", preset, tt.key, *tt.got, tt.want)
		}
	}
}

func TestExpandPreset(t *testing.T) {
	tests := []struct {
		name    string
		preset  string
		wantErr bool
		nested  nestedToggles
		checkFn func(t *testing.T, ops config.Operations)
	}{
		{
			name:   "all preset",
			preset: "all",
			nested: nestedToggles{upsertMany: true, createWithRelated: true, updateWithRelated: true, upsertWithRelated: true},
			checkFn: func(t *testing.T, ops config.Operations) {
				t.Helper()
				if !*ops.Get || !*ops.GetMany || !*ops.Create || !*ops.CreateMany ||
					!*ops.Update || !*ops.UpdateMany || !*ops.UpdateWhere || !*ops.Upsert ||
					!*ops.SoftDelete || !*ops.Restore || !*ops.HardDelete ||
					!*ops.Exists || !*ops.Count || !*ops.Increment ||
					!*ops.Paginate || !*ops.Connection || !*ops.Stream {
					t.Error("all preset should have all operations enabled")
				}
			},
		},
		{
			name:   "read_only preset",
			preset: "read_only",
			nested: nestedToggles{},
			checkFn: func(t *testing.T, ops config.Operations) {
				t.Helper()
				if !*ops.Get || !*ops.GetMany || !*ops.Exists || !*ops.Count || !*ops.Paginate || !*ops.Connection || !*ops.Stream {
					t.Error("read_only should enable read operations including stream")
				}
				if *ops.Create || *ops.CreateMany || *ops.Update || *ops.UpdateMany ||
					*ops.UpdateWhere || *ops.Upsert || *ops.SoftDelete || *ops.Restore ||
					*ops.HardDelete || *ops.Increment {
					t.Error("read_only should disable write operations")
				}
			},
		},
		{
			name:   "append_only preset",
			preset: "append_only",
			// append_only disables update and upsert, so only the
			// create half of the nested surface survives.
			nested: nestedToggles{createWithRelated: true},
			checkFn: func(t *testing.T, ops config.Operations) {
				t.Helper()
				if !*ops.Get || !*ops.GetMany || !*ops.Create || !*ops.CreateMany ||
					!*ops.Exists || !*ops.Count || !*ops.Paginate || !*ops.Connection {
					t.Error("append_only should enable read + create operations")
				}
				if *ops.Update || *ops.UpdateMany || *ops.UpdateWhere || *ops.Upsert ||
					*ops.SoftDelete || *ops.Restore || *ops.HardDelete || *ops.Increment {
					t.Error("append_only should disable update/delete operations")
				}
			},
		},
		{
			name:   "no_delete preset",
			preset: "no_delete",
			// Both delete presets resolve all four exactly as `all` does:
			// the nested methods track the verb they compose, not the
			// delete policy (PRD §4.6 / §9.9).
			nested: nestedToggles{upsertMany: true, createWithRelated: true, updateWithRelated: true, upsertWithRelated: true},
			checkFn: func(t *testing.T, ops config.Operations) {
				t.Helper()
				if !*ops.Get || !*ops.Create || !*ops.Update || !*ops.Upsert || !*ops.Increment {
					t.Error("no_delete should enable all non-delete operations")
				}
				if *ops.SoftDelete || *ops.Restore || *ops.HardDelete {
					t.Error("no_delete should disable all delete operations")
				}
			},
		},
		{
			name:   "no_hard_delete preset",
			preset: "no_hard_delete",
			nested: nestedToggles{upsertMany: true, createWithRelated: true, updateWithRelated: true, upsertWithRelated: true},
			checkFn: func(t *testing.T, ops config.Operations) {
				t.Helper()
				if !*ops.SoftDelete || !*ops.Restore {
					t.Error("no_hard_delete should keep soft delete/restore enabled")
				}
				if *ops.HardDelete {
					t.Error("no_hard_delete should disable hard delete")
				}
			},
		},
		{
			name:    "invalid preset",
			preset:  "write_only",
			wantErr: true,
		},
		{
			name:   "empty preset defaults to all",
			preset: "",
			nested: nestedToggles{upsertMany: true, createWithRelated: true, updateWithRelated: true, upsertWithRelated: true},
			checkFn: func(t *testing.T, ops config.Operations) {
				t.Helper()
				if !*ops.Get || !*ops.Create || !*ops.HardDelete {
					t.Error("empty preset should default to all operations enabled")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ops, err := config.ExpandPreset(tt.preset)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ExpandPreset(%q) expected error, got nil", tt.preset)
				}
				return
			}
			if err != nil {
				t.Fatalf("ExpandPreset(%q) unexpected error: %v", tt.preset, err)
			}
			assertNestedToggles(t, tt.preset, ops, tt.nested)
			tt.checkFn(t, ops)
		})
	}
}

func TestResolveOperations_PresetWithOverrides(t *testing.T) {
	ops := config.Operations{
		Preset:     "all",
		Increment:  new(false),
		HardDelete: new(false),
	}

	resolved, err := config.ResolveOperations(ops)
	if err != nil {
		t.Fatalf("ResolveOperations() unexpected error: %v", err)
	}

	// Overridden to false.
	if *resolved.Increment != false {
		t.Errorf("Increment = %v, want false", *resolved.Increment)
	}
	if *resolved.HardDelete != false {
		t.Errorf("HardDelete = %v, want false", *resolved.HardDelete)
	}

	// Not overridden — should be true from "all" preset.
	if !*resolved.Get {
		t.Error("Get should be true (from 'all' preset)")
	}
	if !*resolved.Create {
		t.Error("Create should be true (from 'all' preset)")
	}
	if !*resolved.SoftDelete {
		t.Error("SoftDelete should be true (from 'all' preset)")
	}
}

func TestResolveOperations_ReadOnlyWithCreateOverride(t *testing.T) {
	ops := config.Operations{
		Preset: "read_only",
		Create: new(true),
	}

	resolved, err := config.ResolveOperations(ops)
	if err != nil {
		t.Fatalf("ResolveOperations() unexpected error: %v", err)
	}

	if !*resolved.Create {
		t.Error("Create should be true (overridden on top of read_only)")
	}
	if *resolved.Update {
		t.Error("Update should be false (from read_only preset)")
	}
}

// TestLeftoverOperationsKeyPointsAtTheAPIMask pins PRD §4.13's leftover-key
// rule. The client has no operations toggle (§4.6), and a config that still
// carries one fails with a message naming the path and api.operations, not the
// strict decoder's bare unknown-field error.
func TestLeftoverOperationsKeyPointsAtTheAPIMask(t *testing.T) {
	tests := []struct {
		name  string
		yaml  string
		wants []string
	}{
		{
			name:  "under generation",
			yaml:  "generation:\n  operations: read_only\n",
			wants: []string{"generation.operations: the Go client generates every method the schema allows; use api.operations"},
		},
		{
			name:  "under a table, as a toggle map",
			yaml:  "tables:\n  products:\n    operations:\n      hard_delete: false\n",
			wants: []string{"tables.products.operations: the Go client generates every method the schema allows; use api.operations"},
		},
		{
			name: "both at once are both reported",
			yaml: "generation:\n  operations: all\ntables:\n  products:\n    operations: read_only\n",
			wants: []string{
				"generation.operations: the Go client generates",
				"tables.products.operations: the Go client generates",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTestConfig(t, "input:\n  dialect: postgres\n  paths: [\"./migrations\"]\noutput:\n  dir: ./models\n"+tt.yaml)
			cfg, err := config.LoadConfig(path)
			if err != nil {
				t.Fatalf("LoadConfig() error = %v, want the key to decode so validation can name it", err)
			}
			_, err = config.ValidatePreParse(cfg)
			if err == nil {
				t.Fatalf("ValidatePreParse() = nil, want the leftover operations key rejected")
			}
			for _, want := range tt.wants {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("ValidatePreParse() error = %q, want it to contain %q", err, want)
				}
			}
		})
	}
}

func TestLoadConfig_APIOperationsAsString(t *testing.T) {
	path := writeTestConfig(t, `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models
api:
  enabled: true
  operations: no_delete
`)

	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}

	if cfg.API.Operations == nil || cfg.API.Operations.Preset != "no_delete" {
		t.Fatalf("API.Operations = %+v, want preset %q", cfg.API.Operations, "no_delete")
	}

	resolved, err := config.ResolveOperations(*cfg.API.Operations)
	if err != nil {
		t.Fatalf("ResolveOperations() unexpected error: %v", err)
	}
	if *resolved.SoftDelete {
		t.Error("no_delete preset should disable SoftDelete")
	}
	if *resolved.HardDelete {
		t.Error("no_delete preset should disable HardDelete")
	}
	if !*resolved.Create {
		t.Error("no_delete preset should keep Create enabled")
	}
}

func TestLoadConfig_EventsEnabledAtRoot(t *testing.T) {
	path := writeTestConfig(t, `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models
events:
  enabled: true
`)

	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}

	if cfg.Events == nil {
		t.Fatal("Events should not be nil")
	}
	if !cfg.Events.Enabled {
		t.Error("Events.Enabled = false, want true")
	}
}

func TestLoadConfig_EventsDisabledAtRoot(t *testing.T) {
	path := writeTestConfig(t, `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models
events:
  enabled: false
`)

	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}

	if cfg.Events == nil {
		t.Fatal("Events should not be nil")
	}
	if cfg.Events.Enabled {
		t.Error("Events.Enabled = true, want false")
	}
}

func TestLoadConfig_EventsDefaultNil(t *testing.T) {
	path := writeTestConfig(t, `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models
`)

	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}

	if cfg.Events != nil {
		t.Errorf("Events = %+v, want nil (events not configured)", cfg.Events)
	}
}

func TestLoadConfig_PerTableEventOverride(t *testing.T) {
	path := writeTestConfig(t, `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models
events:
  enabled: true
tables:
  audit_logs:
    events:
      enabled: false
  products: {}
`)

	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}

	auditLogs := cfg.Tables["audit_logs"]
	if auditLogs.Events == nil {
		t.Fatal("audit_logs.Events should not be nil")
	}
	if auditLogs.Events.Enabled == nil {
		t.Fatal("audit_logs.Events.Enabled should not be nil")
	}
	if *auditLogs.Events.Enabled {
		t.Error("audit_logs.Events.Enabled = true, want false")
	}

	products := cfg.Tables["products"]
	if products.Events != nil {
		t.Errorf("products.Events = %+v, want nil (no per-table override)", products.Events)
	}
}

func TestResolveTableEventsEnabled(t *testing.T) {
	tests := []struct {
		name   string
		table  config.TableConfig
		global *config.EventConfig
		want   bool
	}{
		{
			name:   "no global no table defaults to false",
			table:  config.TableConfig{},
			global: nil,
			want:   false,
		},
		{
			name:   "global enabled no table override",
			table:  config.TableConfig{},
			global: &config.EventConfig{Enabled: true},
			want:   true,
		},
		{
			name:   "global disabled no table override",
			table:  config.TableConfig{},
			global: &config.EventConfig{Enabled: false},
			want:   false,
		},
		{
			name: "table override false takes precedence over global true",
			table: config.TableConfig{
				Events: &config.TableEventConfig{Enabled: new(false)},
			},
			global: &config.EventConfig{Enabled: true},
			want:   false,
		},
		{
			name: "table override true takes precedence over global false",
			table: config.TableConfig{
				Events: &config.TableEventConfig{Enabled: new(true)},
			},
			global: &config.EventConfig{Enabled: false},
			want:   true,
		},
		{
			name: "table override true with nil global",
			table: config.TableConfig{
				Events: &config.TableEventConfig{Enabled: new(true)},
			},
			global: nil,
			want:   true,
		},
		{
			name: "table events struct with nil enabled inherits global true",
			table: config.TableConfig{
				Events: &config.TableEventConfig{Enabled: nil},
			},
			global: &config.EventConfig{Enabled: true},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := config.ResolveTableEventsEnabled(tt.table, tt.global)
			if got != tt.want {
				t.Errorf("ResolveTableEventsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadConfig_CacheRootAndRoundTrip(t *testing.T) {
	path := writeTestConfig(t, `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models
cache:
  enabled: true
  ttl: "30m"
  serializer: msgpack
  key_prefix: myapp
`)

	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}
	if cfg.Cache == nil {
		t.Fatal("Cache should not be nil")
	}
	if !cfg.Cache.Enabled {
		t.Error("Cache.Enabled = false, want true")
	}
	if cfg.Cache.TTL != "30m" {
		t.Errorf("Cache.TTL = %q, want %q", cfg.Cache.TTL, "30m")
	}
	if cfg.Cache.Serializer != config.SerializerMsgpack {
		t.Errorf("Cache.Serializer = %q, want %q", cfg.Cache.Serializer, config.SerializerMsgpack)
	}
	if cfg.Cache.KeyPrefix != "myapp" {
		t.Errorf("Cache.KeyPrefix = %q, want %q", cfg.Cache.KeyPrefix, "myapp")
	}
	if cfg.CacheKeyPrefixDefaulted {
		t.Error("CacheKeyPrefixDefaulted = true, want false for explicit prefix")
	}
	if cfg.Cache.Hydration == nil {
		t.Fatal("Cache.Hydration should be defaulted when cache block is present")
	}
	if cfg.Cache.Hydration.Timeout != "30s" {
		t.Errorf("Cache.Hydration.Timeout = %q, want %q", cfg.Cache.Hydration.Timeout, "30s")
	}
	if !cfg.Cache.Hydration.Enabled {
		t.Error("Cache.Hydration.Enabled = false, want true (default when block unset)")
	}
	if cfg.Cache.CircuitBreaker == nil {
		t.Fatal("Cache.CircuitBreaker should be defaulted when cache block is present")
	}
	if cfg.Cache.CircuitBreaker.FailureThreshold != 5 {
		t.Errorf("Cache.CircuitBreaker.FailureThreshold = %d, want 5", cfg.Cache.CircuitBreaker.FailureThreshold)
	}
	if cfg.Cache.CircuitBreaker.ProbeInterval != "30s" {
		t.Errorf("Cache.CircuitBreaker.ProbeInterval = %q, want %q", cfg.Cache.CircuitBreaker.ProbeInterval, "30s")
	}
	if cfg.Cache.CircuitBreaker.HalfOpenMaxProbes != 1 {
		t.Errorf("Cache.CircuitBreaker.HalfOpenMaxProbes = %d, want 1", cfg.Cache.CircuitBreaker.HalfOpenMaxProbes)
	}
}

func TestLoadConfig_CacheEmptyBlock(t *testing.T) {
	path := writeTestConfig(t, `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models
cache: {}
`)

	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}
	if cfg.Cache == nil {
		t.Fatal("Cache should not be nil when empty block is present")
	}
	if cfg.Cache.Enabled {
		t.Error("Cache.Enabled = true, want false (unset default)")
	}
	if cfg.Cache.TTL != "1h" {
		t.Errorf("Cache.TTL = %q, want %q", cfg.Cache.TTL, "1h")
	}
	if cfg.Cache.Serializer != config.SerializerJSON {
		t.Errorf("Cache.Serializer = %q, want %q", cfg.Cache.Serializer, config.SerializerJSON)
	}
	if cfg.Cache.KeyPrefix != "sqlgen" {
		t.Errorf("Cache.KeyPrefix = %q, want %q", cfg.Cache.KeyPrefix, "sqlgen")
	}
	if !cfg.CacheKeyPrefixDefaulted {
		t.Error("CacheKeyPrefixDefaulted = false, want true (default applied)")
	}
}

func TestLoadConfig_CacheKeyPrefixWarnings(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		wantPrefix  string
		wantWarning bool
	}{
		{
			name: "unset key_prefix defaults and warns",
			yaml: `
cache:
  enabled: true
`,
			wantPrefix:  "sqlgen",
			wantWarning: true,
		},
		{
			name: "explicit empty key_prefix defaults and warns",
			yaml: `
cache:
  enabled: true
  key_prefix: ""
`,
			wantPrefix:  "sqlgen",
			wantWarning: true,
		},
		{
			name: "explicit non-empty key_prefix is verbatim and silent",
			yaml: `
cache:
  enabled: true
  key_prefix: "myapp"
`,
			wantPrefix:  "myapp",
			wantWarning: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTestConfig(t, `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models
`+tt.yaml)

			cfg, err := config.LoadConfig(path)
			if err != nil {
				t.Fatalf("LoadConfig() unexpected error: %v", err)
			}
			if cfg.Cache.KeyPrefix != tt.wantPrefix {
				t.Errorf("Cache.KeyPrefix = %q, want %q", cfg.Cache.KeyPrefix, tt.wantPrefix)
			}

			warnings, err := config.ValidatePreParse(cfg)
			if err != nil {
				t.Fatalf("ValidatePreParse() unexpected error: %v", err)
			}

			gotWarning := false
			for _, w := range warnings {
				if strings.Contains(w.Message, "cache.key_prefix unset") {
					gotWarning = true
					break
				}
			}
			if gotWarning != tt.wantWarning {
				t.Errorf("key_prefix warning emitted = %v, want %v (warnings=%v)", gotWarning, tt.wantWarning, warnings)
			}
		})
	}
}

func TestLoadConfig_CacheTTLDefaultAndExplicit(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		wantTTL time.Duration
	}{
		{
			name: "unset ttl defaults to 1h",
			yaml: `
cache:
  enabled: true
  key_prefix: x
`,
			wantTTL: time.Hour,
		},
		{
			name: "explicit 30m",
			yaml: `
cache:
  enabled: true
  key_prefix: x
  ttl: "30m"
`,
			wantTTL: 30 * time.Minute,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTestConfig(t, `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models
`+tc.yaml)

			cfg, err := config.LoadConfig(path)
			if err != nil {
				t.Fatalf("LoadConfig() unexpected error: %v", err)
			}
			got := config.ResolveTableCacheTTL(config.TableConfig{}, cfg.Cache)
			if got != tc.wantTTL {
				t.Errorf("ResolveTableCacheTTL() = %s, want %s", got, tc.wantTTL)
			}
		})
	}
}

func TestResolveTableCacheEnabled(t *testing.T) {
	tests := []struct {
		name   string
		table  config.TableConfig
		global *config.CacheConfig
		want   bool
	}{
		{
			name:   "no global no table defaults to false",
			table:  config.TableConfig{},
			global: nil,
			want:   false,
		},
		{
			name:   "global enabled no table override",
			table:  config.TableConfig{},
			global: &config.CacheConfig{Enabled: true},
			want:   true,
		},
		{
			name:   "global disabled no table override",
			table:  config.TableConfig{},
			global: &config.CacheConfig{Enabled: false},
			want:   false,
		},
		{
			name: "table override false beats global true",
			table: config.TableConfig{
				Cache: &config.TableCacheConfig{Enabled: new(false)},
			},
			global: &config.CacheConfig{Enabled: true},
			want:   false,
		},
		{
			name: "table override true opts in when global disabled",
			table: config.TableConfig{
				Cache: &config.TableCacheConfig{Enabled: new(true)},
			},
			global: &config.CacheConfig{Enabled: false},
			want:   true,
		},
		{
			name: "table cache struct with nil enabled inherits global true",
			table: config.TableConfig{
				Cache: &config.TableCacheConfig{Enabled: nil},
			},
			global: &config.CacheConfig{Enabled: true},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := config.ResolveTableCacheEnabled(tt.table, tt.global)
			if got != tt.want {
				t.Errorf("ResolveTableCacheEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveTableCacheTTL(t *testing.T) {
	globalTTL := "1h"
	tableTTL := "30m"

	global := &config.CacheConfig{Enabled: true, TTL: globalTTL, Serializer: config.SerializerJSON}

	// Table override wins.
	tbl := config.TableConfig{
		Cache: &config.TableCacheConfig{TTL: &tableTTL},
	}
	if got := config.ResolveTableCacheTTL(tbl, global); got != 30*time.Minute {
		t.Errorf("ResolveTableCacheTTL(override) = %s, want 30m", got)
	}

	// Nil table fields inherit global.
	tbl2 := config.TableConfig{Cache: &config.TableCacheConfig{}}
	if got := config.ResolveTableCacheTTL(tbl2, global); got != time.Hour {
		t.Errorf("ResolveTableCacheTTL(inherit) = %s, want 1h", got)
	}
}

func TestResolveViewCacheEnabled(t *testing.T) {
	tests := []struct {
		name   string
		view   config.ViewConfig
		global *config.CacheConfig
		want   bool
	}{
		{
			name:   "no global no view returns false",
			view:   config.ViewConfig{},
			global: nil,
			want:   false,
		},
		{
			name:   "global enabled does NOT inherit to views (opt-in rule)",
			view:   config.ViewConfig{},
			global: &config.CacheConfig{Enabled: true},
			want:   false,
		},
		{
			name: "view cache struct with nil enabled does not inherit",
			view: config.ViewConfig{
				Cache: &config.ViewCacheConfig{Enabled: nil},
			},
			global: &config.CacheConfig{Enabled: true},
			want:   false,
		},
		{
			name: "view cache enabled explicit true returns true",
			view: config.ViewConfig{
				Cache: &config.ViewCacheConfig{Enabled: new(true)},
			},
			global: &config.CacheConfig{Enabled: false},
			want:   true,
		},
		{
			name: "view cache enabled explicit false returns false",
			view: config.ViewConfig{
				Cache: &config.ViewCacheConfig{Enabled: new(false)},
			},
			global: &config.CacheConfig{Enabled: true},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := config.ResolveViewCacheEnabled(tt.view, tt.global)
			if got != tt.want {
				t.Errorf("ResolveViewCacheEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveViewCacheTTL(t *testing.T) {
	global := &config.CacheConfig{Enabled: true, TTL: "1h", Serializer: config.SerializerJSON}
	ttl := "15m"

	view := config.ViewConfig{
		Cache: &config.ViewCacheConfig{
			Enabled: new(true),
			TTL:     &ttl,
		},
	}
	if got := config.ResolveViewCacheTTL(view, global); got != 15*time.Minute {
		t.Errorf("ResolveViewCacheTTL() = %s, want 15m", got)
	}

	// Inherit from global when view fields nil.
	view2 := config.ViewConfig{Cache: &config.ViewCacheConfig{Enabled: new(true)}}
	if got := config.ResolveViewCacheTTL(view2, global); got != time.Hour {
		t.Errorf("ResolveViewCacheTTL(inherit) = %s, want 1h", got)
	}
}

// TestMatchExcludeTablePattern pins the exclude_tables glob match helper.
func TestMatchExcludeTablePattern(t *testing.T) {
	cases := []struct {
		name     string
		table    string
		schema   string
		patterns []string
		wantHit  bool
	}{
		{name: "exact match", table: "report_acl", patterns: []string{"report_acl"}, wantHit: true},
		{name: "star prefix", table: "temp_aux", patterns: []string{"temp_*"}, wantHit: true},
		{name: "qmark single char", table: "tbl_a", patterns: []string{"tbl_?"}, wantHit: true},
		{name: "qmark too short", table: "tbl_ab", patterns: []string{"tbl_?"}, wantHit: false},
		{name: "no match", table: "users", patterns: []string{"temp_*"}, wantHit: false},
		{name: "empty patterns", table: "anything", patterns: nil, wantHit: false},
		{name: "schema-qualified", table: "trail", schema: "audit", patterns: []string{"audit.*"}, wantHit: true},
		{name: "case sensitive", table: "TEMP_FOO", patterns: []string{"temp_*"}, wantHit: false},
		{name: "second pattern matches", table: "report_acl", patterns: []string{"temp_*", "report_*"}, wantHit: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, got := config.MatchExcludeTablePattern(c.table, c.schema, c.patterns)
			if got != c.wantHit {
				t.Errorf("MatchExcludeTablePattern(%q, %q, %v) = %v, want %v", c.table, c.schema, c.patterns, got, c.wantHit)
			}
		})
	}
}

// TestLoadConfig_TopLevelExcludeTables pins the exclude_tables YAML round-trip.
func TestLoadConfig_TopLevelExcludeTables(t *testing.T) {
	path := writeTestConfig(t, `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models
exclude_tables:
  - temp_*
  - _internal_*
  - report_acl
`)

	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}

	want := []string{"temp_*", "_internal_*", "report_acl"}
	if !slices.Equal(cfg.ExcludeTables, want) {
		t.Errorf("ExcludeTables = %v, want %v", cfg.ExcludeTables, want)
	}
}

// TestLoadConfig_PrimaryKeyColumns pins the primary_key.columns YAML round-trip:
// tables.<name>.primary_key.columns: [a, b] survives Marshal/Unmarshal in
// declaration order.
func TestLoadConfig_PrimaryKeyColumns(t *testing.T) {
	path := writeTestConfig(t, `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models
tables:
  baseline_counter:
    primary_key:
      columns: [organization_id]
  join_asset_ppa:
    primary_key:
      columns: [asset_id, ppa_id]
      strategy: caller
`)

	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}

	single, ok := cfg.Tables["baseline_counter"]
	if !ok {
		t.Fatal("baseline_counter table missing from cfg.Tables")
	}
	if single.PrimaryKey == nil {
		t.Fatal("baseline_counter.PrimaryKey should not be nil")
	}
	if got, want := single.PrimaryKey.Columns, []string{"organization_id"}; !slices.Equal(got, want) {
		t.Errorf("baseline_counter.PrimaryKey.Columns = %v, want %v", got, want)
	}

	composite, ok := cfg.Tables["join_asset_ppa"]
	if !ok {
		t.Fatal("join_asset_ppa table missing from cfg.Tables")
	}
	if composite.PrimaryKey == nil {
		t.Fatal("join_asset_ppa.PrimaryKey should not be nil")
	}
	if got, want := composite.PrimaryKey.Columns, []string{"asset_id", "ppa_id"}; !slices.Equal(got, want) {
		t.Errorf("join_asset_ppa.PrimaryKey.Columns = %v, want %v (declaration order is load-bearing)", got, want)
	}
	if got, want := composite.PrimaryKey.Strategy, config.PKStrategyCaller; got != want {
		t.Errorf("join_asset_ppa.PrimaryKey.Strategy = %q, want %q (strategy + columns must coexist)", got, want)
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := config.LoadConfig("/nonexistent/path/sqlgen.yml")
	if err == nil {
		t.Error("LoadConfig() should return error for nonexistent file")
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	path := writeTestConfig(t, `
input:
  dialect: [invalid yaml
`)

	_, err := config.LoadConfig(path)
	if err == nil {
		t.Error("LoadConfig() should return error for invalid YAML")
	}
}
