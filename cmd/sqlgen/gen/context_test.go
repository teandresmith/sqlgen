package gen_test

import (
	"maps"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gotype"
	"github.com/teandresmith/sqlgen/parser"
	"github.com/teandresmith/sqlgen/sql"
)

// --- Helper constructors ---

func testInput(schema *parser.Schema) *gen.GenerateInput {
	cfg := &config.RootConfig{}
	// Apply defaults to avoid nil pointer issues.
	cfg.Input.Dialect = "postgres"
	cfg.Output.Driver = "pgx"
	cfg.Output.Package = "db"
	cfg.Generation.QueryLimit = new(1000)
	cfg.Generation.BatchSize = new(200)
	cfg.Generation.PageSize = new(100)
	cfg.Generation.CursorKeys = []string{"id"}
	cfg.Generation.UUIDVersion = "v4"
	cfg.Generation.StrictUpdates = new(true)
	cfg.Generation.SoftDeleteColumns = []config.SoftDeleteConfig{
		{Name: "deleted_at", Type: "timestamp"},
	}
	cfg.Generation.UpdateColumns = []string{"updated_at"}
	cfg.Tables = make(map[string]config.TableConfig)
	cfg.Views = make(map[string]config.ViewConfig)
	cfg.Extras = make(map[string]config.ExtraType)
	cfg.Overrides.Types = make(map[string]config.TypeOverride)
	cfg.Overrides.UsePointers = new(true)

	resolver := gotype.NewResolver("postgres", true, cfg.Overrides.Types)

	return &gen.GenerateInput{
		Schema:   schema,
		Config:   cfg,
		Resolver: resolver,
	}
}

// --- Enum context tests ---

func TestBuildEnumContexts(t *testing.T) {
	schema := &parser.Schema{
		Enums: []parser.Enum{
			{Name: "order_status", Schema: "public", Values: []string{"pending", "shipped", "delivered"}},
			{Name: "user_role", Schema: "public", Values: []string{"admin", "user"}},
		},
	}

	contexts := gen.BuildEnumContexts(schema, nil, &config.RootConfig{Input: config.InputConfig{Dialect: config.DialectPostgres}})

	if len(contexts) != 2 {
		t.Fatalf("BuildEnumContexts() returned %d contexts, want 2", len(contexts))
	}

	// Sorted by name.
	if contexts[0].Name != "order_status" {
		t.Errorf("contexts[0].Name = %q, want %q", contexts[0].Name, "order_status")
	}
	if contexts[0].GoTypeName != "OrderStatus" {
		t.Errorf("contexts[0].GoTypeName = %q, want %q", contexts[0].GoTypeName, "OrderStatus")
	}
	if diff := cmp.Diff([]string{"pending", "shipped", "delivered"}, contexts[0].Values); diff != "" {
		t.Errorf("contexts[0].Values mismatch (-want +got):\n%s", diff)
	}

	if contexts[1].Name != "user_role" {
		t.Errorf("contexts[1].Name = %q, want %q", contexts[1].Name, "user_role")
	}
	if contexts[1].GoTypeName != "UserRole" {
		t.Errorf("contexts[1].GoTypeName = %q, want %q", contexts[1].GoTypeName, "UserRole")
	}
}

// --- Type context tests ---

func TestBuildTypeContext_composites(t *testing.T) {
	input := testInput(&parser.Schema{
		CompositeTypes: []parser.CompositeType{
			{
				Name:   "address",
				Schema: "public",
				Attributes: []parser.Attribute{
					{Name: "street", Type: "text"},
					{Name: "city", Type: "text"},
					{Name: "zip_code", Type: "varchar"},
				},
			},
		},
	})

	tc := gen.BuildTypeContext(input, nil)

	if len(tc.Composites) != 1 {
		t.Fatalf("Composites has %d entries, want 1", len(tc.Composites))
	}

	ct := tc.Composites[0]
	if ct.GoTypeName != "Address" {
		t.Errorf("GoTypeName = %q, want %q", ct.GoTypeName, "Address")
	}
	if len(ct.Fields) != 3 {
		t.Fatalf("Fields has %d entries, want 3", len(ct.Fields))
	}
	if ct.Fields[0].FieldName != "Street" {
		t.Errorf("Fields[0].FieldName = %q, want %q", ct.Fields[0].FieldName, "Street")
	}
	if ct.Fields[0].GoType != "string" {
		t.Errorf("Fields[0].GoType = %q, want %q", ct.Fields[0].GoType, "string")
	}
}

func TestBuildTypeContext_domains(t *testing.T) {
	input := testInput(&parser.Schema{
		DomainTypes: []parser.DomainType{
			{Name: "email", Schema: "public", BaseType: "text"},
		},
	})

	tc := gen.BuildTypeContext(input, nil)

	if len(tc.Domains) != 1 {
		t.Fatalf("Domains has %d entries, want 1", len(tc.Domains))
	}
	if tc.Domains[0].GoTypeName != "Email" {
		t.Errorf("GoTypeName = %q, want %q", tc.Domains[0].GoTypeName, "Email")
	}
	if tc.Domains[0].BaseGoType != "string" {
		t.Errorf("BaseGoType = %q, want %q", tc.Domains[0].BaseGoType, "string")
	}
}

func TestBuildTypeContext_extras(t *testing.T) {
	input := testInput(&parser.Schema{})
	input.Config.Extras = map[string]config.ExtraType{
		"metadata": {
			Description: "Generic metadata object.",
			Fields: map[string]config.ExtraTypeField{
				"key":   {Type: "string", Tags: map[string]string{"json": "key"}},
				"value": {Type: "string", Tags: map[string]string{"json": "value"}},
			},
		},
	}

	tc := gen.BuildTypeContext(input, nil)

	if len(tc.Extras) != 1 {
		t.Fatalf("Extras has %d entries, want 1", len(tc.Extras))
	}
	extra := tc.Extras[0]
	if extra.GoTypeName != "Metadata" {
		t.Errorf("GoTypeName = %q, want %q", extra.GoTypeName, "Metadata")
	}
	if extra.Description != "Generic metadata object." {
		t.Errorf("Description = %q, want %q", extra.Description, "Generic metadata object.")
	}
	if len(extra.Fields) != 2 {
		t.Fatalf("Fields has %d entries, want 2", len(extra.Fields))
	}
	// Fields sorted by name: key, value.
	if extra.Fields[0].FieldName != "Key" {
		t.Errorf("Fields[0].FieldName = %q, want %q", extra.Fields[0].FieldName, "Key")
	}
}

// --- Doc comment resolution tests ---

func TestDocCommentResolution_configOverridesSQL(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:    "products",
				Comment: "SQL comment on products",
				Columns: []parser.Column{
					{Name: "id", Type: "bigserial", PrimaryKey: true, Comment: "SQL comment on id"},
					{Name: "name", Type: "text", Comment: "SQL comment on name"},
				},
			},
		},
	}

	input := testInput(schema)
	input.Config.Tables["products"] = config.TableConfig{
		Description: "Config description for products",
		ColumnMap: map[string]config.ColumnOverride{
			"name": {Description: "Config description for name"},
		},
	}

	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}
	if len(contexts) != 1 {
		t.Fatalf("BuildTableContexts() returned %d contexts, want 1", len(contexts))
	}

	tc := contexts[0]

	// Table description: config > SQL
	if tc.Description != "Config description for products" {
		t.Errorf("Table description = %q, want %q", tc.Description, "Config description for products")
	}

	// Column "name": config > SQL
	var nameCol *gen.ColumnContext
	for i := range tc.Columns {
		if tc.Columns[i].Name == "name" {
			nameCol = &tc.Columns[i]
		}
	}
	if nameCol == nil {
		t.Fatal("column 'name' not found")
	}
	if nameCol.Description != "Config description for name" {
		t.Errorf("Column name description = %q, want %q", nameCol.Description, "Config description for name")
	}
}

func TestDocCommentResolution_SQLWhenNoConfig(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:    "products",
				Comment: "Catalog of products available for sale.",
				Columns: []parser.Column{
					{Name: "id", Type: "bigserial", PrimaryKey: true},
					{Name: "name", Type: "text", Comment: "Display name of the product."},
				},
			},
		},
	}

	input := testInput(schema)
	// No config description set — SQL comments are passed through verbatim.

	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}

	tc := contexts[0]
	if tc.Description != "Catalog of products available for sale." {
		t.Errorf("Table description = %q, want %q", tc.Description, "Catalog of products available for sale.")
	}

	var nameCol *gen.ColumnContext
	for i := range tc.Columns {
		if tc.Columns[i].Name == "name" {
			nameCol = &tc.Columns[i]
		}
	}
	if nameCol == nil {
		t.Fatal("column 'name' not found")
	}
	if nameCol.Description != "Display name of the product." {
		t.Errorf("Column description = %q, want %q", nameCol.Description, "Display name of the product.")
	}
}

func TestDocCommentResolution_noneWhenNeitherExists(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "bigserial", PrimaryKey: true},
					{Name: "name", Type: "text"},
				},
			},
		},
	}

	input := testInput(schema)

	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}

	tc := contexts[0]
	if tc.Description != "" {
		t.Errorf("Table description = %q, want empty", tc.Description)
	}

	var nameCol *gen.ColumnContext
	for i := range tc.Columns {
		if tc.Columns[i].Name == "name" {
			nameCol = &tc.Columns[i]
		}
	}
	if nameCol == nil {
		t.Fatal("column 'name' not found")
	}
	if nameCol.Description != "" {
		t.Errorf("Column description = %q, want empty", nameCol.Description)
	}
}

// --- PK strategy detection tests ---

func TestPKStrategyDetection(t *testing.T) {
	tests := []struct {
		name     string
		dialect  config.Dialect
		columns  []parser.Column
		override *config.TablePrimaryKeyConfig
		want     config.PKStrategy
	}{
		{
			name:    "postgres serial PK → db",
			dialect: "postgres",
			columns: []parser.Column{
				{Name: "id", Type: "serial", PrimaryKey: true, Default: "nextval('id_seq')"},
			},
			want: config.PKStrategyDB,
		},
		{
			name:    "postgres UUID with default → db",
			dialect: "postgres",
			columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true, Default: "gen_random_uuid()"},
			},
			want: config.PKStrategyDB,
		},
		{
			name:    "postgres UUID no default → app",
			dialect: "postgres",
			columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
			},
			want: config.PKStrategyApp,
		},
		{
			name:    "mysql auto_increment → db",
			dialect: "mysql",
			columns: []parser.Column{
				{Name: "id", Type: "bigint", PrimaryKey: true, AutoIncrement: true},
			},
			want: config.PKStrategyDB,
		},
		{
			name:    "mysql UUID with default → app",
			dialect: "mysql",
			columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true, Default: "UUID()"},
			},
			want: config.PKStrategyApp,
		},
		{
			name:    "mysql UUID no default → app",
			dialect: "mysql",
			columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
			},
			want: config.PKStrategyApp,
		},
		{
			name:    "mysql no auto-gen → caller",
			dialect: "mysql",
			columns: []parser.Column{
				{Name: "id", Type: "varchar", PrimaryKey: true},
			},
			want: config.PKStrategyCaller,
		},
		{
			name:    "sqlite autoincrement → db",
			dialect: "sqlite",
			columns: []parser.Column{
				{Name: "id", Type: "integer", PrimaryKey: true, AutoIncrement: true},
			},
			want: config.PKStrategyDB,
		},
		{
			name:    "sqlite UUID no default → app",
			dialect: "sqlite",
			columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true},
			},
			want: config.PKStrategyApp,
		},
		{
			name:    "explicit override to caller",
			dialect: "postgres",
			columns: []parser.Column{
				{Name: "id", Type: "serial", PrimaryKey: true, Default: "nextval('id_seq')"},
			},
			override: &config.TablePrimaryKeyConfig{Strategy: "caller"},
			want:     config.PKStrategyCaller,
		},
		// A single-column PK that is also a foreign key is
		// caller-supplied on every dialect — its value must equal an existing
		// parent row's key, so nothing may mint it. Before the fix each of
		// these classified on SQL type alone and resolved to app (or db).
		{
			name:    "postgres FK PK, UUID no default → caller",
			dialect: "postgres",
			columns: []parser.Column{
				{Name: "user_id", Type: "uuid", PrimaryKey: true, FKReference: &parser.FKReference{Table: "users", Column: "id"}},
			},
			want: config.PKStrategyCaller,
		},
		{
			name:    "postgres FK PK, UUID with default → caller",
			dialect: "postgres",
			columns: []parser.Column{
				{Name: "user_id", Type: "uuid", PrimaryKey: true, Default: "gen_random_uuid()", FKReference: &parser.FKReference{Table: "users", Column: "id"}},
			},
			want: config.PKStrategyCaller,
		},
		{
			name:    "mysql FK PK, UUID → caller",
			dialect: "mysql",
			columns: []parser.Column{
				{Name: "user_id", Type: "uuid", PrimaryKey: true, FKReference: &parser.FKReference{Table: "users", Column: "id"}},
			},
			want: config.PKStrategyCaller,
		},
		{
			name:    "sqlite FK PK, integer autoincrement → caller",
			dialect: "sqlite",
			columns: []parser.Column{
				{Name: "user_id", Type: "integer", PrimaryKey: true, AutoIncrement: true, FKReference: &parser.FKReference{Table: "users", Column: "id"}},
			},
			want: config.PKStrategyCaller,
		},
		{
			// A referenced column list is optional in SQLite DDL
			// (`REFERENCES users`), which leaves Column empty on a perfectly
			// real foreign key. Detection must key on the reference existing,
			// never on Column being populated.
			name:    "sqlite FK PK with no referenced column → caller",
			dialect: "sqlite",
			columns: []parser.Column{
				{Name: "user_id", Type: "uuid", PrimaryKey: true, FKReference: &parser.FKReference{Table: "users"}},
			},
			want: config.PKStrategyCaller,
		},
		{
			// The mirror image: a synthetic reference carries no referential
			// guarantee, so it must not move a surrogate key off db.
			name:    "synthetic FK on surrogate PK stays db",
			dialect: "postgres",
			columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true, Default: "gen_random_uuid()", FKReference: &parser.FKReference{Table: "orders", Synthetic: true}},
			},
			want: config.PKStrategyDB,
		},
		{
			// An explicit strategy still wins over the FK clause — the
			// documented escape hatch for schemas that mint the child key
			// first under a DEFERRABLE constraint.
			name:    "explicit override beats FK PK detection",
			dialect: "postgres",
			columns: []parser.Column{
				{Name: "user_id", Type: "uuid", PrimaryKey: true, FKReference: &parser.FKReference{Table: "users", Column: "id"}},
			},
			override: &config.TablePrimaryKeyConfig{Strategy: "app"},
			want:     config.PKStrategyApp,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &parser.Schema{
				Tables: []parser.Table{
					{Name: "test_table", Columns: tt.columns},
				},
			}
			input := testInput(schema)
			input.Config.Input.Dialect = tt.dialect

			if tt.override != nil {
				input.Config.Tables["test_table"] = config.TableConfig{
					PrimaryKey: tt.override,
				}
			}

			// Re-create resolver for the right dialect.
			input.Resolver = gotype.NewResolver(tt.dialect, true, input.Config.Overrides.Types)

			contexts, err := gen.BuildTableContexts(input, nil)
			if err != nil {
				t.Fatalf("BuildTableContexts() error: %v", err)
			}
			if len(contexts) != 1 {
				t.Fatalf("got %d contexts, want 1", len(contexts))
			}
			if contexts[0].PKStrategy != tt.want {
				t.Errorf("PKStrategy = %q, want %q", contexts[0].PKStrategy, tt.want)
			}
		})
	}
}

// TestFKPrimaryKeyCreateInput pins the FK-primary-key surface: the
// PK of a 1:1 extension table must be a required, caller-supplied field on the
// create input. Under the pre-fix `app` classification it was an
// omittable.Value[T] that Create silently filled with a freshly minted UUID —
// a value no parent row could ever hold.
func TestFKPrimaryKeyCreateInput(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{Name: "users", Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true, Default: "gen_random_uuid()"},
			}},
			{Name: "profiles", Columns: []parser.Column{
				{Name: "user_id", Type: "uuid", PrimaryKey: true, FKReference: &parser.FKReference{Table: "users", Column: "id"}},
				{Name: "bio", Type: "text", Nullable: true},
			}},
		},
	}

	contexts, err := gen.BuildTableContexts(testInput(schema), nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() unexpected error: %v", err)
	}

	tc, ok := tableContextByName(contexts, "profiles")
	if !ok {
		t.Fatalf("BuildTableContexts() did not emit a context for profiles")
	}
	if tc.PKStrategy != config.PKStrategyCaller {
		t.Fatalf("profiles PKStrategy = %q, want %q", tc.PKStrategy, config.PKStrategyCaller)
	}

	var field gen.InputFieldContext
	for _, f := range tc.CreateInputFields {
		if f.ColumnName == "user_id" {
			field = f
		}
	}
	if field.ColumnName == "" {
		t.Fatalf("CreateInputFields is missing user_id, got %+v", tc.CreateInputFields)
	}
	if !field.Required || field.Omittable {
		t.Errorf("CreateInputFields[user_id] required = %v, omittable = %v, want true, false",
			field.Required, field.Omittable)
	}
	if field.GoType != "uuid.UUID" {
		t.Errorf("CreateInputFields[user_id] GoType = %q, want %q", field.GoType, "uuid.UUID")
	}
}

// TestConfigDeclaredFKDoesNotDemoteSurrogatePK guards the FK-primary-key false
// positive. applyConfigDeclaredFKs resolves a `relationships.fk` against the
// *related* table and marks any column matching that name — including that
// table's own surrogate PK when the names collide. Such a reference is
// synthetic and carries no referential guarantee, so it must leave the strategy
// alone.
func TestConfigDeclaredFKDoesNotDemoteSurrogatePK(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{Name: "customers", Columns: []parser.Column{
				{Name: "customer_id", Type: "uuid", PrimaryKey: true, Default: "gen_random_uuid()"},
			}},
			{Name: "orders", Columns: []parser.Column{
				{Name: "id", Type: "uuid", PrimaryKey: true, Default: "gen_random_uuid()"},
				{Name: "customer_id", Type: "uuid"},
			}},
		},
	}
	input := testInput(schema)
	input.Config.Tables["orders"] = config.TableConfig{
		Relationships: []config.TableRelationship{
			{Name: "Customer", Type: "o2o", Table: "customers", FK: "customer_id"},
		},
	}

	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() unexpected error: %v", err)
	}

	tc, ok := tableContextByName(contexts, "customers")
	if !ok {
		t.Fatalf("BuildTableContexts() did not emit a context for customers")
	}
	if tc.PKStrategy != config.PKStrategyDB {
		t.Errorf("customers PKStrategy = %q, want %q (synthetic FK must not demote a surrogate key)",
			tc.PKStrategy, config.PKStrategyDB)
	}
}

func tableContextByName(contexts []gen.TableContext, name string) (gen.TableContext, bool) {
	for _, tc := range contexts {
		if tc.TableName == name {
			return tc, true
		}
	}
	return gen.TableContext{}, false
}

// --- Comparator type selection tests ---

func TestComparatorTypeSelection(t *testing.T) {
	tests := []struct {
		name    string
		col     gen.ColumnContext
		dialect config.Dialect
		want    string
	}{
		{
			name:    "string PK → ID",
			col:     gen.ColumnContext{GoType: "string", PrimaryKey: true},
			dialect: "postgres",
			want:    "*comparator.ID",
		},
		{
			name:    "string FK → ID",
			col:     gen.ColumnContext{GoType: "string", FKReference: &gen.FKReferenceContext{Table: "users"}},
			dialect: "postgres",
			want:    "*comparator.ID",
		},
		{
			name:    "regular string → String",
			col:     gen.ColumnContext{GoType: "string"},
			dialect: "postgres",
			want:    "*comparator.String",
		},
		{
			name:    "nullable string → NullableString",
			col:     gen.ColumnContext{GoType: "*string", Nullable: true},
			dialect: "postgres",
			want:    "*comparator.NullableString",
		},
		{
			name:    "int64 → Number[int64]",
			col:     gen.ColumnContext{GoType: "int64"},
			dialect: "postgres",
			want:    "*comparator.Number[int64]",
		},
		{
			name:    "bool → Bool",
			col:     gen.ColumnContext{GoType: "bool"},
			dialect: "postgres",
			want:    "*comparator.Bool",
		},
		{
			name:    "time.Time → Time",
			col:     gen.ColumnContext{GoType: "time.Time"},
			dialect: "postgres",
			want:    "*comparator.Time",
		},
		{
			name:    "postgres jsonb (map[string]any) → JSONB",
			col:     gen.ColumnContext{GoType: "map[string]any", SQLType: "jsonb"},
			dialect: "postgres",
			want:    "*comparator.JSONB",
		},
		{
			name:    "postgres jsonb (types.JSON) → JSONB",
			col:     gen.ColumnContext{GoType: "types.JSON", SQLType: "jsonb"},
			dialect: "postgres",
			want:    "*comparator.JSONB",
		},
		{
			name:    "postgres nullable jsonb (types.JSON) → NullableJSONB",
			col:     gen.ColumnContext{GoType: "types.JSON", SQLType: "jsonb", Nullable: true},
			dialect: "postgres",
			want:    "*comparator.NullableJSONB",
		},
		{
			name:    "postgres json (types.JSON) → JSON",
			col:     gen.ColumnContext{GoType: "types.JSON", SQLType: "json"},
			dialect: "postgres",
			want:    "*comparator.JSON",
		},
		{
			name:    "mysql json (map[string]any) → JSON",
			col:     gen.ColumnContext{GoType: "map[string]any", SQLType: "json"},
			dialect: "mysql",
			want:    "*comparator.JSON",
		},
		{
			name:    "mysql json (types.JSON) → JSON",
			col:     gen.ColumnContext{GoType: "types.JSON", SQLType: "json"},
			dialect: "mysql",
			want:    "*comparator.JSON",
		},
		{
			name:    "mysql nullable json (types.JSON) → NullableJSON",
			col:     gen.ColumnContext{GoType: "types.JSON", SQLType: "json", Nullable: true},
			dialect: "mysql",
			want:    "*comparator.NullableJSON",
		},
		// Lock-in: decimal.Decimal does not satisfy comparator.Numeric
		// (`~int | … | ~float64`), so it must route to the String comparator —
		// even though the increment-eligibility predicate includes
		// decimal. This guards against accidental conflation of the
		// two predicates: increment routes through SQL-side `column = column +
		// ?`, while comparator routes through Go-side type constraints.
		{
			name:    "decimal.Decimal → String (lock-in)",
			col:     gen.ColumnContext{GoType: "decimal.Decimal"},
			dialect: "postgres",
			want:    "*comparator.String",
		},
		{
			name:    "*decimal.Decimal nullable → NullableString (lock-in)",
			col:     gen.ColumnContext{GoType: "*decimal.Decimal", Nullable: true},
			dialect: "postgres",
			want:    "*comparator.NullableString",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use the exported resolveComparatorType via context builder.
			// We test it indirectly via BuildFilterFields or directly via the funcmap.
			fields := []gen.ColumnContext{tt.col}
			schema := &parser.Schema{
				Tables: []parser.Table{
					{
						Name:    "test",
						Columns: []parser.Column{{Name: "col", Type: "text", PrimaryKey: tt.col.PrimaryKey, Nullable: tt.col.Nullable}},
					},
				},
			}

			input := testInput(schema)
			input.Config.Input.Dialect = tt.dialect

			// Build table contexts and check the filter field.
			// For simplicity, test via BuildTableContexts.
			_ = fields
			_ = schema
			_ = input

			// Direct funcmap test.
			d := dialectFor(tt.dialect)
			fm := gen.FuncMap(d)
			comparatorType := fm["comparatorType"].(func(gen.ColumnContext) string)
			got := comparatorType(tt.col)
			if got != tt.want {
				t.Errorf("comparatorType() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- parenIfComposite ---

// Regression: composite-literal zero values (e.g. `uuid.UUID{}`,
// `decimal.Decimal{}`) in expression position inside `if` headers must be
// parenthesized so the Go parser doesn't consume the trailing `{` as the
// if-body open brace. Sentinel zero values (`0`, `""`, `nil`, `uuid.Nil`) do
// not need parens — wrapping them would produce gratuitous golden churn.
func TestParenIfComposite(t *testing.T) {
	tests := []struct {
		name      string
		zeroValue string
		want      string
	}{
		{name: "composite_struct_uuid", zeroValue: "uuid.UUID{}", want: "(uuid.UUID{})"},
		{name: "composite_struct_decimal", zeroValue: "decimal.Decimal{}", want: "(decimal.Decimal{})"},
		{name: "composite_pointer_struct", zeroValue: "decimal.NullDecimal{}", want: "(decimal.NullDecimal{})"},
		{name: "sentinel_uuid_nil", zeroValue: "uuid.Nil", want: "uuid.Nil"},
		{name: "primitive_int_zero", zeroValue: "0", want: "0"},
		{name: "primitive_string_empty", zeroValue: `""`, want: `""`},
		{name: "primitive_bool_false", zeroValue: "false", want: "false"},
		{name: "pointer_nil", zeroValue: "nil", want: "nil"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gen.ParenIfCompositeForTest(tt.zeroValue)
			if got != tt.want {
				t.Errorf("parenIfComposite(%q) = %q, want %q", tt.zeroValue, got, tt.want)
			}
		})
	}
}

// --- CreateInput field classification tests ---

func TestCreateInputFieldClassification(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "bigserial", PrimaryKey: true, Default: "nextval('id_seq')"},
					{Name: "name", Type: "text"},                             // required: NOT NULL, no DEFAULT
					{Name: "price", Type: "numeric"},                         // required: NOT NULL, no DEFAULT
					{Name: "status", Type: "text", Default: "'active'"},      // omittable: has DEFAULT
					{Name: "notes", Type: "text", Nullable: true},            // omittable: nullable
					{Name: "total", Type: "numeric", GeneratedExpr: "price"}, // excluded: computed
					{Name: "updated_at", Type: "timestamptz"},                // excluded: update_column
				},
			},
		},
	}

	input := testInput(schema)

	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}

	tc := contexts[0]

	// PK excluded (auto-increment/serial PK, strategy db).
	for _, f := range tc.CreateInputFields {
		if f.ColumnName == "id" {
			t.Error("id should be excluded from CreateInput (auto-increment PK strategy db)")
		}
	}
	// Computed excluded.
	for _, f := range tc.CreateInputFields {
		if f.ColumnName == "total" {
			t.Error("total should be excluded from CreateInput (computed)")
		}
	}

	// Check field map.
	fieldMap := make(map[string]gen.InputFieldContext)
	for _, f := range tc.CreateInputFields {
		fieldMap[f.ColumnName] = f
	}

	nameField, ok := fieldMap["name"]
	if !ok {
		t.Fatal("name field missing from CreateInput")
	}
	if !nameField.Required {
		t.Error("name field should be Required")
	}
	if nameField.GoType != "string" {
		t.Errorf("name field GoType = %q, want %q", nameField.GoType, "string")
	}

	priceField, ok := fieldMap["price"]
	if !ok {
		t.Fatal("price field missing from CreateInput")
	}
	if !priceField.Required {
		t.Error("price field should be Required")
	}

	statusField, ok := fieldMap["status"]
	if !ok {
		t.Fatal("status field missing from CreateInput")
	}
	if !statusField.Omittable {
		t.Error("status field should be Omittable (has DEFAULT)")
	}

	notesField, ok := fieldMap["notes"]
	if !ok {
		t.Fatal("notes field missing from CreateInput")
	}
	if !notesField.Omittable {
		t.Error("notes field should be Omittable (nullable)")
	}

	// Update column now included as omittable (supports data backfilling).
	updatedAtField, ok := fieldMap["updated_at"]
	if !ok {
		t.Fatal("updated_at field missing from CreateInput (should be omittable, not excluded)")
	}
	if !updatedAtField.Omittable {
		t.Error("updated_at field should be Omittable (update_column)")
	}

	// JSONTag populated for all fields.
	for _, f := range tc.CreateInputFields {
		if f.JSONTag != f.ColumnName {
			t.Errorf("CreateInput field %q JSONTag = %q, want %q", f.ColumnName, f.JSONTag, f.ColumnName)
		}
	}
}

func TestCreateInputFieldClassification_callerPK(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "external_imports",
				Columns: []parser.Column{
					{Name: "id", Type: "varchar", PrimaryKey: true},
					{Name: "name", Type: "text"},
				},
			},
		},
	}

	input := testInput(schema)
	input.Config.Tables["external_imports"] = config.TableConfig{
		PrimaryKey: &config.TablePrimaryKeyConfig{Strategy: "caller"},
	}

	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}

	tc := contexts[0]

	// PK should be required with caller strategy.
	var found bool
	for _, f := range tc.CreateInputFields {
		if f.ColumnName == "id" {
			found = true
			if !f.Required {
				t.Error("id should be Required with caller PK strategy")
			}
		}
	}
	if !found {
		t.Error("id field missing from CreateInput with caller PK strategy")
	}
}

// --- UpdateInput field classification tests ---

func TestUpdateInputFieldClassification(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "bigserial", PrimaryKey: true, Default: "nextval('id_seq')"},
					{Name: "name", Type: "text"},
					{Name: "price", Type: "numeric"},
					{Name: "total", Type: "numeric", GeneratedExpr: "price"},
					{Name: "updated_at", Type: "timestamptz"},
				},
			},
		},
	}

	input := testInput(schema)

	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}

	tc := contexts[0]

	// PK excluded.
	for _, f := range tc.UpdateInputFields {
		if f.ColumnName == "id" {
			t.Error("id should be excluded from UpdateInput")
		}
	}
	// Computed excluded.
	for _, f := range tc.UpdateInputFields {
		if f.ColumnName == "total" {
			t.Error("total should be excluded from UpdateInput")
		}
	}

	// Update columns included as omittable (user can override, else auto-set to time.Now()).
	foundUpdatedAt := false
	for _, f := range tc.UpdateInputFields {
		if f.ColumnName == "updated_at" {
			foundUpdatedAt = true
			if !f.Omittable {
				t.Error("updated_at should be Omittable in UpdateInput")
			}
		}
	}
	if !foundUpdatedAt {
		t.Error("updated_at should be included in UpdateInput (user can override auto-set)")
	}

	// All remaining fields should be omittable.
	for _, f := range tc.UpdateInputFields {
		if !f.Omittable {
			t.Errorf("UpdateInput field %q should be Omittable", f.ColumnName)
		}
	}

	// Check field count: name, price, updated_at.
	if len(tc.UpdateInputFields) != 3 {
		t.Errorf("UpdateInputFields has %d entries, want 3 (name, price, updated_at)", len(tc.UpdateInputFields))
	}

	// JSONTag populated for all fields.
	for _, f := range tc.UpdateInputFields {
		if f.JSONTag != f.ColumnName {
			t.Errorf("UpdateInput field %q JSONTag = %q, want %q", f.ColumnName, f.JSONTag, f.ColumnName)
		}
	}
}

// --- IncrementColumn detection tests ---

func TestIncrementColumnDetection(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "bigserial", PrimaryKey: true, Default: "nextval('id_seq')"},
					{Name: "name", Type: "text"},
					{Name: "price", Type: "numeric"},
					{Name: "stock", Type: "integer"},
				},
			},
		},
	}

	input := testInput(schema)

	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}

	tc := contexts[0]

	// PK excluded from increment.
	for _, col := range tc.IncrementColumns {
		if col.Name == "id" {
			t.Error("id (PK) should be excluded from IncrementColumns")
		}
	}

	// Non-numeric excluded.
	for _, col := range tc.IncrementColumns {
		if col.Name == "name" {
			t.Error("name (string) should be excluded from IncrementColumns")
		}
	}

	// price and stock should be present.
	names := make(map[string]bool)
	for _, col := range tc.IncrementColumns {
		names[col.Name] = true
	}
	if !names["price"] {
		t.Error("price should be in IncrementColumns")
	}
	if !names["stock"] {
		t.Error("stock should be in IncrementColumns")
	}
}

// --- Conflict target tests ---

func TestConflictTargetGeneration(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "bigserial", PrimaryKey: true, Default: "nextval('id_seq')"},
					{Name: "sku", Type: "varchar", Unique: true},
					{Name: "name", Type: "text"},
				},
				Constraints: []parser.Constraint{
					{Type: parser.PrimaryKey, Columns: []string{"id"}},
					{Type: parser.Unique, Columns: []string{"sku"}},
				},
			},
		},
	}

	input := testInput(schema)

	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}

	tc := contexts[0]

	if len(tc.ConflictTargets) != 2 {
		t.Fatalf("ConflictTargets has %d entries, want 2", len(tc.ConflictTargets))
	}

	// Sorted by constant name.
	targetMap := make(map[string]gen.ConflictTargetContext)
	for _, ct := range tc.ConflictTargets {
		targetMap[ct.ConstantName] = ct
	}

	pk, ok := targetMap["ProductConflictPK"]
	if !ok {
		t.Fatal("ProductConflictPK not found")
	}
	if diff := cmp.Diff([]string{"id"}, pk.Columns); diff != "" {
		t.Errorf("PK columns mismatch (-want +got):\n%s", diff)
	}

	sku, ok := targetMap["ProductConflictSKU"]
	if !ok {
		t.Fatal("ProductConflictSKU not found")
	}
	if diff := cmp.Diff([]string{"sku"}, sku.Columns); diff != "" {
		t.Errorf("SKU columns mismatch (-want +got):\n%s", diff)
	}
}

func TestConflictTargetInlineUnique(t *testing.T) {
	// Inline UNIQUE columns (col.Unique = true) without a table-level
	// Constraint entry — this is how MySQL and PostgreSQL parsers represent
	// column-level UNIQUE declarations.
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true, AutoIncrement: true},
					{Name: "sku", Type: "varchar", Unique: true},
					{Name: "email", Type: "varchar", Unique: true},
					{Name: "name", Type: "text"},
				},
				Constraints: []parser.Constraint{
					{Type: parser.PrimaryKey, Columns: []string{"id"}},
				},
			},
		},
	}

	input := testInput(schema)

	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}

	tc := contexts[0]

	if len(tc.ConflictTargets) != 3 {
		t.Fatalf("ConflictTargets has %d entries, want 3", len(tc.ConflictTargets))
	}

	targetMap := make(map[string]gen.ConflictTargetContext)
	for _, ct := range tc.ConflictTargets {
		targetMap[ct.ConstantName] = ct
	}

	if _, ok := targetMap["ProductConflictPK"]; !ok {
		t.Fatal("ProductConflictPK not found")
	}
	if _, ok := targetMap["ProductConflictSKU"]; !ok {
		t.Fatal("ProductConflictSKU not found")
	}
	if _, ok := targetMap["ProductConflictEmail"]; !ok {
		t.Fatal("ProductConflictEmail not found")
	}
}

// --- View context tests ---

func TestBuildViewContexts(t *testing.T) {
	schema := &parser.Schema{
		Views: []parser.View{
			{
				Name:   "product_summary",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint"},
					{Name: "name", Type: "text"},
					{Name: "total_orders", Type: "bigint"},
				},
			},
		},
	}

	input := testInput(schema)

	contexts, err := gen.BuildViewContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildViewContexts() error: %v", err)
	}

	if len(contexts) != 1 {
		t.Fatalf("BuildViewContexts() returned %d contexts, want 1", len(contexts))
	}

	vc := contexts[0]
	if vc.StructName != "ProductSummary" {
		t.Errorf("StructName = %q, want %q", vc.StructName, "ProductSummary")
	}
	if vc.ViewName != "product_summary" {
		t.Errorf("ViewName = %q, want %q", vc.ViewName, "product_summary")
	}
	if len(vc.Columns) != 3 {
		t.Errorf("Columns has %d entries, want 3", len(vc.Columns))
	}
	if vc.HasPK {
		t.Error("HasPK should be false (no @pk annotation)")
	}
}

// TestBuildViewContexts_arrayAgg pins the chain an ARRAY_AGG view column has to
// travel: the parser hands codegen the array SQL type plus the aggregate, the
// resolver turns that into a Go slice and marks it IsSlice, and only that flag
// earns the stdlib driver's pq.Array scan wrapper. A slice type that reaches the
// model any other way carries IsSlice=false, takes the bare &v.Field target and
// fails at scan time with "unsupported Scan".
func TestBuildViewContexts_arrayAgg(t *testing.T) {
	tests := []struct {
		name         string
		dialect      config.Dialect
		driver       config.Driver
		wantGoType   string
		wantIsSlice  bool
		wantScanExpr string
	}{
		{
			name:         "postgres stdlib wraps the array in pq.Array",
			dialect:      config.DialectPostgres,
			driver:       "stdlib",
			wantGoType:   "[]string",
			wantIsSlice:  true,
			wantScanExpr: "pq.Array(&p.ProductNames)",
		},
		{
			name:         "postgres pgx scans the array natively",
			dialect:      config.DialectPostgres,
			driver:       "pgx",
			wantGoType:   "[]string",
			wantIsSlice:  true,
			wantScanExpr: "&p.ProductNames",
		},
		{
			// Neither MySQL nor SQLite has ARRAY_AGG or a SQL array type, so an
			// array SQL type would miss every builtin mapping and land on the
			// resolver's unknown-type "string" fallback. Fail closed instead.
			name:         "mysql falls back to []any rather than binding string",
			dialect:      config.DialectMySQL,
			driver:       "stdlib",
			wantGoType:   "[]any",
			wantIsSlice:  false,
			wantScanExpr: "&p.ProductNames",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &parser.Schema{
				Views: []parser.View{
					{
						Name: "product_rollup",
						Columns: []parser.Column{
							{Name: "category", Type: "text"},
							{Name: "product_names", Type: "text[]", Aggregate: "ARRAY_AGG"},
						},
					},
				},
			}

			input := testInput(schema)
			input.Config.Input.Dialect = tt.dialect
			input.Config.Output.Driver = tt.driver
			input.Resolver = gotype.NewResolver(tt.dialect, true, input.Config.Overrides.Types)

			contexts, err := gen.BuildViewContexts(input, nil)
			if err != nil {
				t.Fatalf("BuildViewContexts() error: %v", err)
			}
			if len(contexts) != 1 {
				t.Fatalf("BuildViewContexts() returned %d contexts, want 1", len(contexts))
			}

			var col gen.ColumnContext
			for _, c := range contexts[0].Columns {
				if c.Name == "product_names" {
					col = c
					break
				}
			}
			if col.Name == "" {
				t.Fatal("view context has no product_names column")
			}

			if col.GoType != tt.wantGoType {
				t.Errorf("GoType = %q, want %q", col.GoType, tt.wantGoType)
			}
			if col.IsSlice != tt.wantIsSlice {
				t.Errorf("IsSlice = %v, want %v", col.IsSlice, tt.wantIsSlice)
			}

			var scanExpr string
			for _, sh := range contexts[0].ScanShapes {
				if sh.ColumnName == "product_names" {
					scanExpr = sh.ScanExpr
					break
				}
			}
			if scanExpr != tt.wantScanExpr {
				t.Errorf("ScanExpr = %q, want %q", scanExpr, tt.wantScanExpr)
			}
		})
	}
}

// TestBuildViewContexts_enumSliceColumn pins that a view column resolving to a
// named enum slice keeps its element type. Without SliceElemType the filter
// instantiates comparator.Slice[UserRoleSlice] — a []UserRole, which is not
// comparable, so the generated package does not compile — and the scan shape
// wraps a type that already has its own Scan/Value in pq.Array. The table path
// has always carried the field; views dropped it.
func TestBuildViewContexts_enumSliceColumn(t *testing.T) {
	schema := &parser.Schema{
		Views: []parser.View{
			{
				Name: "warehouse_roles",
				Columns: []parser.Column{
					{Name: "name", Type: "text"},
					{Name: "allowed_roles", Type: "user_role[]"},
				},
			},
		},
	}

	input := testInput(schema)
	input.Resolver = gotype.NewResolver(config.DialectPostgres, true, input.Config.Overrides.Types)
	input.Resolver.RegisterEnum("user_role", "UserRole")
	input.Config.Output.Driver = "stdlib"

	contexts, err := gen.BuildViewContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildViewContexts() error: %v", err)
	}
	if len(contexts) != 1 {
		t.Fatalf("BuildViewContexts() returned %d contexts, want 1", len(contexts))
	}

	var col gen.ColumnContext
	for _, c := range contexts[0].Columns {
		if c.Name == "allowed_roles" {
			col = c
			break
		}
	}
	if col.Name == "" {
		t.Fatal("view context has no allowed_roles column")
	}

	if col.GoType != "UserRoleSlice" {
		t.Errorf("GoType = %q, want %q", col.GoType, "UserRoleSlice")
	}
	if !col.IsSlice {
		t.Error("IsSlice = false, want true")
	}
	if col.SliceElemType != "UserRole" {
		t.Errorf("SliceElemType = %q, want %q", col.SliceElemType, "UserRole")
	}

	// A named slice brings its own Scan/Value, so it must NOT be wrapped.
	var scanExpr string
	for _, sh := range contexts[0].ScanShapes {
		if sh.ColumnName == "allowed_roles" {
			scanExpr = sh.ScanExpr
			break
		}
	}
	if want := "&w.AllowedRoles"; scanExpr != want {
		t.Errorf("ScanExpr = %q, want %q", scanExpr, want)
	}
}

// --- Client context tests ---

func TestBuildClientContext(t *testing.T) {
	tables := []gen.TableContext{
		{StructName: "Product"},
		{StructName: "Order"},
	}
	views := []gen.ViewContext{
		{StructName: "ProductSummary"},
	}

	ctx := gen.BuildClientContext(tables, views, "db", "Client", false, nil, nil, nil)

	if len(ctx.Entities) != 3 {
		t.Fatalf("Entities has %d entries, want 3", len(ctx.Entities))
	}

	// Sorted by struct name: Order, Product, ProductSummary.
	if ctx.Entities[0].StructName != "Order" {
		t.Errorf("Entities[0].StructName = %q, want %q", ctx.Entities[0].StructName, "Order")
	}
	if ctx.Entities[0].InterfaceName != "OrderClient" {
		t.Errorf("Entities[0].InterfaceName = %q, want %q", ctx.Entities[0].InterfaceName, "OrderClient")
	}
	if ctx.Entities[0].AccessorName != "Orders" {
		t.Errorf("Entities[0].AccessorName = %q, want %q", ctx.Entities[0].AccessorName, "Orders")
	}
	if ctx.Entities[0].IsView {
		t.Error("Entities[0] should not be a view")
	}

	if ctx.Entities[1].AccessorName != "Products" {
		t.Errorf("Entities[1].AccessorName = %q, want %q", ctx.Entities[1].AccessorName, "Products")
	}

	if ctx.Entities[2].StructName != "ProductSummary" {
		t.Errorf("Entities[2].StructName = %q, want %q", ctx.Entities[2].StructName, "ProductSummary")
	}
	if ctx.Entities[2].AccessorName != "ProductSummary" {
		t.Errorf("Entities[2].AccessorName = %q, want %q", ctx.Entities[2].AccessorName, "ProductSummary")
	}
	if !ctx.Entities[2].IsView {
		t.Error("Entities[2] should be a view")
	}
}

// --- Representative schema test ---

func TestBuildTableContexts_representativeSchema(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "products",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "name", Type: "text"},
					{Name: "price", Type: "numeric"},
					{Name: "sku", Type: "varchar", Unique: true},
					{Name: "deleted_at", Type: "timestamptz", Nullable: true},
					{Name: "updated_at", Type: "timestamptz"},
				},
				Constraints: []parser.Constraint{
					{Type: parser.Unique, Columns: []string{"sku"}},
				},
			},
		},
	}

	input := testInput(schema)

	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}

	tc := contexts[0]

	// Struct name.
	if tc.StructName != "Product" {
		t.Errorf("StructName = %q, want %q", tc.StructName, "Product")
	}

	// PK strategy: postgres UUID no default → app.
	if tc.PKStrategy != config.PKStrategyApp {
		t.Errorf("PKStrategy = %q, want %q", tc.PKStrategy, config.PKStrategyApp)
	}

	// Soft delete detected.
	if tc.SoftDelete == nil {
		t.Fatal("SoftDelete should be detected (deleted_at)")
	}
	if tc.SoftDelete.Column != "deleted_at" {
		t.Errorf("SoftDelete.Column = %q, want %q", tc.SoftDelete.Column, "deleted_at")
	}
	if tc.SoftDelete.Strategy != "timestamp" {
		t.Errorf("SoftDelete.Strategy = %q, want %q", tc.SoftDelete.Strategy, "timestamp")
	}

	// Update columns detected.
	if len(tc.UpdateColumns) != 1 || tc.UpdateColumns[0].Name != "updated_at" {
		t.Errorf("UpdateColumns = %v, want [updated_at]", tc.UpdateColumns)
	}

	// Composite PK.
	if tc.CompositePK {
		t.Error("CompositePK should be false (single PK)")
	}

	// Columns count (all 6 included).
	if len(tc.Columns) != 6 {
		t.Errorf("Columns has %d entries, want 6", len(tc.Columns))
	}

	// Filter fields count.
	if len(tc.FilterFields) != 6 {
		t.Errorf("FilterFields has %d entries, want 6", len(tc.FilterFields))
	}

	// Scan shapes count.
	if len(tc.ScanShapes) != 6 {
		t.Errorf("ScanShapes has %d entries, want 6", len(tc.ScanShapes))
	}

	// Imports collected from columns.
	if len(tc.Imports) == 0 {
		t.Error("Imports should be non-empty (uuid, time types present)")
	}
}

func TestBuildTableContexts_relationshipDescription(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "company_id", Type: "uuid", Nullable: true, FKReference: &parser.FKReference{Table: "companies", Column: "id"}},
				},
			},
			{
				Name: "companies",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
				},
			},
			{
				Name: "reviews",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "product_id", Type: "uuid", FKReference: &parser.FKReference{Table: "products", Column: "id"}},
				},
			},
		},
		Relationships: []parser.Relationship{
			{
				Name:        "company",
				SourceTable: "products",
				TargetTable: "companies",
				Type:        parser.OneToOne,
				FKColumn:    "company_id",
			},
			{
				Name:        "reviews",
				SourceTable: "products",
				TargetTable: "reviews",
				Type:        parser.OneToMany,
				FKColumn:    "product_id",
			},
		},
	}

	input := testInput(schema)

	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}

	// Find the products table context.
	var productCtx gen.TableContext
	for _, tc := range contexts {
		if tc.TableName == "products" {
			productCtx = tc
			break
		}
	}

	if len(productCtx.Relationships) != 2 {
		t.Fatalf("Relationships count = %d, want 2", len(productCtx.Relationships))
	}

	// Sorted by name: company, reviews.
	if productCtx.Relationships[0].Description != "one-to-one relationship with the companies table." {
		t.Errorf("Relationship[0].Description = %q, want one-to-one description", productCtx.Relationships[0].Description)
	}
	if productCtx.Relationships[1].Description != "one-to-many relationship with the reviews table." {
		t.Errorf("Relationship[1].Description = %q, want one-to-many description", productCtx.Relationships[1].Description)
	}
}

// Regression: FKNullable must be wired from the FK column on the
// target side (O2M/O2O) or junction (M2M). Without it the relationship-load
// filter emits a non-nullable comparator that does not match the target's
// filter struct field type.
func TestBuildTableContexts_relationshipFKNullable(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "categories",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "parent_id", Type: "bigint", Nullable: true, FKReference: &parser.FKReference{Table: "categories", Column: "id"}},
					{Name: "name", Type: "text"},
				},
			},
			{
				Name:   "posts",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "author_id", Type: "bigint", FKReference: &parser.FKReference{Table: "users", Column: "id"}},
				},
			},
			{
				Name:   "users",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
				},
			},
		},
		Relationships: []parser.Relationship{
			// Self-reference with nullable FK on the child side.
			{
				Name:        "children",
				SourceTable: "public.categories",
				TargetTable: "public.categories",
				Type:        parser.OneToMany,
				FKColumn:    "parent_id",
			},
			// Plain O2M with non-nullable FK on the child side.
			{
				Name:        "posts",
				SourceTable: "public.users",
				TargetTable: "public.posts",
				Type:        parser.OneToMany,
				FKColumn:    "author_id",
			},
		},
	}

	input := testInput(schema)
	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}

	rel := func(table, name string) gen.RelationshipContext {
		t.Helper()
		for _, tc := range contexts {
			if tc.TableName != table {
				continue
			}
			for _, r := range tc.Relationships {
				if r.Name == name {
					return r
				}
			}
		}
		t.Fatalf("relationship %q not found on table %q", name, table)
		return gen.RelationshipContext{}
	}

	// Self-referencing children: parent_id is nullable → FKNullable=true.
	if got := rel("categories", "children"); !got.FKNullable {
		t.Errorf("categories.children FKNullable = false, want true (parent_id is nullable)")
	}

	// users → posts: author_id is NOT NULL → FKNullable=false.
	if got := rel("users", "posts"); got.FKNullable {
		t.Errorf("users.posts FKNullable = true, want false (author_id is NOT NULL)")
	}
}

// O2O FKNullable must be resolved against whichever table
// actually carries the FK column. Auto-detected (addO2O) edges hold the FK on
// the *source*/parent table; config edges hold it on the *target*. The wiring
// previously looked up the FK on the target unconditionally, so auto-detected
// O2O edges silently resolved FKNullable to its zero value (false). No template
// consumes O2O FKNullable today, so this is a correct-by-construction fix rather
// than an observable output change — pin it so a future O2O consumer inherits
// the right value.
func TestBuildTableContexts_relationshipFKNullable_O2O(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "users",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					// Nullable FK on the source side — the auto-detected O2O shape.
					{Name: "primary_profile_id", Type: "bigint", Nullable: true, FKReference: &parser.FKReference{Table: "profiles", Column: "id"}},
				},
			},
			{
				Name:   "profiles",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
				},
			},
			{
				Name:   "assets",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
				},
			},
			{
				Name:   "documents",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					// Nullable FK on the target side — the config O2O shape.
					{Name: "entity_id", Type: "bigint", Nullable: true},
				},
			},
		},
		Relationships: []parser.Relationship{
			// Auto-detected O2O: FK (primary_profile_id) lives on the source
			// (users). Target lookup (profiles) misses → must fall back to the
			// parent table to read the column's nullability.
			{
				Name:        "primary_profile",
				SourceTable: "public.users",
				TargetTable: "public.profiles",
				Type:        parser.OneToOne,
				Side:        parser.SideParent,
				FKColumn:    "primary_profile_id",
			},
			// Config-shaped O2O: FK (entity_id) lives on the target (documents).
			// Target lookup must win — the parent (assets) has no such column.
			{
				Name:        "primary_document",
				SourceTable: "public.assets",
				TargetTable: "public.documents",
				Type:        parser.OneToOne,
				Side:        parser.SideParent,
				FKColumn:    "entity_id",
			},
		},
	}

	input := testInput(schema)
	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}

	rel := func(table, name string) gen.RelationshipContext {
		t.Helper()
		for _, tc := range contexts {
			if tc.TableName != table {
				continue
			}
			for _, r := range tc.Relationships {
				if r.Name == name {
					return r
				}
			}
		}
		t.Fatalf("relationship %q not found on table %q", name, table)
		return gen.RelationshipContext{}
	}

	// Auto-detected O2O: users.primary_profile_id is nullable, resolved via the
	// parent-table fallback. Pre-fix this returned false (target lookup miss).
	if got := rel("users", "primary_profile"); !got.FKNullable {
		t.Errorf("users.primary_profile FKNullable = false, want true (primary_profile_id is nullable, FK on source)")
	}

	// Config-shaped O2O: documents.entity_id is nullable, resolved via the
	// target lookup (assets carries no entity_id).
	if got := rel("assets", "primary_document"); !got.FKNullable {
		t.Errorf("assets.primary_document FKNullable = false, want true (entity_id is nullable, FK on target)")
	}
}

// PRD §13.1: FKOnTarget is the structural answer to "which table physically
// holds the FK column", derived once when the relationship context is built.
// The O2O join builder used to re-derive it inline by scanning the target's
// columns and then throw it away; the write-side emitters need the same fact,
// and a second derivation is free to drift from the first.
//
// Side does not answer this question and is untouched by the hoist: addO2O and
// addO2M both hard-code SideParent, and a config-declared edge defaults to
// "parent" while putting the FK on the target — the shape of the graphql
// example's assets.PrimaryDocument, asserted below.
func TestBuildTableContexts_relationshipFKOnTarget(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "users",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
				},
			},
			{
				// Auto-detected O2O: the unique FK lives here, on the source.
				Name:   "profiles",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "user_id", Type: "uuid", Unique: true, InlineUnique: true, FKReference: &parser.FKReference{Table: "users", Schema: "public", Column: "id"}},
				},
			},
			{
				// Auto-detected O2M: the non-unique FK lives on the many side.
				Name:   "posts",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "author_id", Type: "uuid", FKReference: &parser.FKReference{Table: "users", Schema: "public", Column: "id"}},
				},
			},
			{
				Name:   "tags",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
				},
			},
			{
				// Auto-detected M2M: both FKs live on the junction, so neither
				// end of the edge holds one.
				Name:   "post_tags",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "post_id", Type: "uuid", PrimaryKey: true, FKReference: &parser.FKReference{Table: "posts", Schema: "public", Column: "id"}},
					{Name: "tag_id", Type: "uuid", PrimaryKey: true, FKReference: &parser.FKReference{Table: "tags", Schema: "public", Column: "id"}},
				},
				Constraints: []parser.Constraint{
					{Type: parser.PrimaryKey, Columns: []string{"post_id", "tag_id"}},
				},
			},
			{
				Name:   "assets",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
				},
			},
			{
				// Config-declared O2O: entity_id lives on the target.
				Name:   "documents",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "entity_id", Type: "uuid", Nullable: true},
					{Name: "entity_type", Type: "text"},
				},
			},
		},
	}

	input := testInput(schema)
	input.Config.Tables["public.assets"] = config.TableConfig{
		Relationships: []config.TableRelationship{
			{
				Name:  "PrimaryDocument",
				Type:  "one_to_one",
				Table: "documents",
				FK:    "entity_id",
				Discriminator: &config.RelationshipDiscriminator{
					Column: "entity_type",
					Value:  "asset.primary",
				},
			},
		},
	}
	parser.DetectRelationships(input.Schema)

	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}

	// Takes its own *testing.T rather than closing over the outer one: every
	// caller below is inside a subtest, and a t.Fatalf on the parent from a
	// subtest goroutine aborts the wrong test (guidelines/TESTING.md §8).
	rel := func(t *testing.T, table, field string) gen.RelationshipContext {
		t.Helper()
		for _, tc := range contexts {
			if tc.TableName != table {
				continue
			}
			for _, r := range tc.Relationships {
				if r.FieldName == field {
					return r
				}
			}
			t.Fatalf("relationship field %q not found on table %q; got %v", field, table, fieldNamesOf(tc.Relationships))
		}
		t.Fatalf("no context emitted for table %q", table)
		return gen.RelationshipContext{}
	}

	tests := []struct {
		name   string
		table  string
		field  string
		want   bool
		reason string
	}{
		{
			name:   "auto-detected O2O holds the FK on the source",
			table:  "profiles",
			field:  "Users",
			want:   false,
			reason: "profiles.user_id is on the source table, not on users",
		},
		{
			name:   "config-declared O2O holds the FK on the target",
			table:  "assets",
			field:  "PrimaryDocument",
			want:   true,
			reason: "documents.entity_id is on the target table, not on assets",
		},
		{
			name:   "O2M holds the FK on the many side",
			table:  "users",
			field:  "Posts",
			want:   true,
			reason: "posts.author_id is on the target table",
		},
		{
			name:   "M2M holds no FK on either end",
			table:  "posts",
			field:  "Tags",
			want:   false,
			reason: "both FKs live on the post_tags junction",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rel(t, tt.table, tt.field).FKOnTarget; got != tt.want {
				t.Errorf("%s.%s FKOnTarget = %v, want %v (%s)", tt.table, tt.field, got, tt.want, tt.reason)
			}
		})
	}

	// The two O2O edges disagree on FKOnTarget while agreeing on Side, which is
	// the whole reason Side cannot stand in for it (PRD §13.1).
	t.Run("both O2O edges declare the same Side", func(t *testing.T) {
		for _, c := range []struct{ table, field string }{{"profiles", "Users"}, {"assets", "PrimaryDocument"}} {
			if got := rel(t, c.table, c.field).Side; got != parser.SideParent {
				t.Errorf("%s.%s Side = %v, want SideParent", c.table, c.field, got)
			}
		}
	})
}

// Chained O2O joins are built from contexts that findO2ORelationships creates by
// calling relationshipToContext / configRelationshipToContext *directly*, not
// through buildRelationshipContexts. Deriving FKOnTarget anywhere but inside
// those two constructors leaves a chained edge at the zero value, which silently
// inverts its JOIN ON clause. Pin the ON clause of a chain whose second hop
// holds its FK on the target.
func TestBuildO2OJoinDetails_chainedFKOnTarget(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "orders",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "customer_id", Type: "uuid", Unique: true, InlineUnique: true, FKReference: &parser.FKReference{Table: "customers", Schema: "public", Column: "id"}},
				},
			},
			{
				Name:   "customers",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
				},
			},
			{
				Name:   "addresses",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "customer_id", Type: "uuid", Nullable: true},
				},
			},
		},
	}

	input := testInput(schema)
	input.Config.Tables["public.customers"] = config.TableConfig{
		Relationships: []config.TableRelationship{
			{Name: "PrimaryAddress", Type: "one_to_one", Table: "addresses", FK: "customer_id"},
		},
	}
	parser.DetectRelationships(input.Schema)

	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}

	var orders gen.TableContext
	for _, tc := range contexts {
		if tc.TableName == "orders" {
			orders = tc
		}
	}
	if len(orders.O2OJoinDetails) != 1 {
		t.Fatalf("orders O2OJoinDetails = %d, want 1", len(orders.O2OJoinDetails))
	}
	hop1 := orders.O2OJoinDetails[0]
	if len(hop1.ChainedJoins) != 1 {
		t.Fatalf("orders → customers ChainedJoins = %d, want 1", len(hop1.ChainedJoins))
	}

	// Hop 1 (orders → customers) holds its FK on the source: orders.customer_id = customers.id.
	if hop1.OnLocal != "customer_id" || hop1.OnRemote != "id" {
		t.Errorf("orders → customers ON = %s/%s, want customer_id/id (FK on the source)", hop1.OnLocal, hop1.OnRemote)
	}
	// Hop 2 (customers → addresses) holds its FK on the target: customers.id = addresses.customer_id.
	hop2 := hop1.ChainedJoins[0]
	if hop2.OnLocal != "id" || hop2.OnRemote != "customer_id" {
		t.Errorf("customers → addresses ON = %s/%s, want id/customer_id (FK on the target)", hop2.OnLocal, hop2.OnRemote)
	}
}

// A has-one edge holds its FK on the target, so the ON clause's parent side is
// the column that FK points at: the parent's PK, which is what a nested
// has-one create writes into the FK (PRD §9.9.1 shape 2). Reading the target's
// PK name there instead only works while both tables spell their key the same;
// users.user_id ← profiles.owner_id with profiles.id joined on "u.id", a column
// users does not have. The target-side PK stays the scan / HasData key. The
// chained hop pins that the parent side follows the recursion: profiles →
// badges hangs off profiles, so its parent column is profiles.id.
func TestBuildO2OJoinDetails_fkOnTargetParentPKName(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "users",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "user_id", Type: "uuid", PrimaryKey: true},
				},
			},
			{
				Name:   "profiles",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "owner_id", Type: "uuid", Nullable: true},
				},
			},
			{
				Name:   "badges",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "badge_key", Type: "uuid", PrimaryKey: true},
					{Name: "profile_id", Type: "uuid", Nullable: true},
				},
			},
		},
	}

	input := testInput(schema)
	input.Config.Tables["public.users"] = config.TableConfig{
		Relationships: []config.TableRelationship{
			{Name: "Profile", Type: "one_to_one", Table: "profiles", FK: "owner_id"},
		},
	}
	input.Config.Tables["public.profiles"] = config.TableConfig{
		Relationships: []config.TableRelationship{
			{Name: "Badge", Type: "one_to_one", Table: "badges", FK: "profile_id"},
		},
	}
	parser.DetectRelationships(input.Schema)

	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}

	var users gen.TableContext
	for _, tc := range contexts {
		if tc.TableName == "users" {
			users = tc
		}
	}
	if len(users.O2OJoinDetails) != 1 {
		t.Fatalf("users O2OJoinDetails = %d, want 1", len(users.O2OJoinDetails))
	}
	hop1 := users.O2OJoinDetails[0]
	if hop1.OnLocal != "user_id" || hop1.OnRemote != "owner_id" || hop1.PKColumn != "id" {
		t.Errorf("users → profiles ON/PK = %s/%s/%s, want user_id/owner_id/id (parent PK = target FK; target PK scans)", hop1.OnLocal, hop1.OnRemote, hop1.PKColumn)
	}
	if len(hop1.ChainedJoins) != 1 {
		t.Fatalf("users → profiles ChainedJoins = %d, want 1", len(hop1.ChainedJoins))
	}
	hop2 := hop1.ChainedJoins[0]
	if hop2.OnLocal != "id" || hop2.OnRemote != "profile_id" || hop2.PKColumn != "badge_key" {
		t.Errorf("profiles → badges ON/PK = %s/%s/%s, want id/profile_id/badge_key (parent PK = target FK; target PK scans)", hop2.OnLocal, hop2.OnRemote, hop2.PKColumn)
	}
}

// --- CreateInput UUID PK classification tests ---

func TestCreateInputFieldClassification_UUIDPKStrategyDB(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true, Default: "gen_random_uuid()"},
					{Name: "name", Type: "text"},
				},
			},
		},
	}

	input := testInput(schema)

	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}

	tc := contexts[0]
	if tc.PKStrategy != config.PKStrategyDB {
		t.Fatalf("PKStrategy = %q, want %q", tc.PKStrategy, config.PKStrategyDB)
	}

	fieldMap := make(map[string]gen.InputFieldContext)
	for _, f := range tc.CreateInputFields {
		fieldMap[f.ColumnName] = f
	}

	idField, ok := fieldMap["id"]
	if !ok {
		t.Fatal("id field missing from CreateInput (UUID PK strategy db should be omittable, not excluded)")
	}
	if !idField.Omittable {
		t.Error("id field should be Omittable (UUID PK strategy db)")
	}
}

func TestCreateInputFieldClassification_UUIDPKStrategyApp(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "name", Type: "text"},
				},
			},
		},
	}

	input := testInput(schema)

	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}

	tc := contexts[0]
	if tc.PKStrategy != config.PKStrategyApp {
		t.Fatalf("PKStrategy = %q, want %q", tc.PKStrategy, config.PKStrategyApp)
	}

	fieldMap := make(map[string]gen.InputFieldContext)
	for _, f := range tc.CreateInputFields {
		fieldMap[f.ColumnName] = f
	}

	idField, ok := fieldMap["id"]
	if !ok {
		t.Fatal("id field missing from CreateInput (UUID PK strategy app should be omittable, not excluded)")
	}
	if !idField.Omittable {
		t.Error("id field should be Omittable (UUID PK strategy app)")
	}
}

func TestCreateInputFieldClassification_autoIncrementPKExcluded(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "bigserial", PrimaryKey: true, Default: "nextval('id_seq')"},
					{Name: "name", Type: "text"},
				},
			},
		},
	}

	input := testInput(schema)

	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}

	tc := contexts[0]

	for _, f := range tc.CreateInputFields {
		if f.ColumnName == "id" {
			t.Error("id should be excluded from CreateInput (auto-increment PK strategy db)")
		}
	}
}

func TestCreateInputFieldClassification_updateColumnsOmittable(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "bigserial", PrimaryKey: true, Default: "nextval('id_seq')"},
					{Name: "name", Type: "text"},
					{Name: "updated_at", Type: "timestamptz"},
				},
			},
		},
	}

	input := testInput(schema)

	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}

	tc := contexts[0]

	fieldMap := make(map[string]gen.InputFieldContext)
	for _, f := range tc.CreateInputFields {
		fieldMap[f.ColumnName] = f
	}

	updatedAt, ok := fieldMap["updated_at"]
	if !ok {
		t.Fatal("updated_at field missing from CreateInput (update_column should be omittable, not excluded)")
	}
	if !updatedAt.Omittable {
		t.Error("updated_at field should be Omittable (update_column)")
	}
}

// --- UpdateInput update_columns classification test ---

func TestUpdateInputFieldClassification_updateColumnsExcluded(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "bigserial", PrimaryKey: true, Default: "nextval('id_seq')"},
					{Name: "name", Type: "text"},
					{Name: "updated_at", Type: "timestamptz"},
				},
			},
		},
	}

	input := testInput(schema)

	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}

	tc := contexts[0]

	// update_columns should be included in UpdateInput as omittable (user can override, else auto-set).
	foundUpdatedAt := false
	for _, f := range tc.UpdateInputFields {
		if f.ColumnName == "updated_at" {
			foundUpdatedAt = true
			if !f.Omittable {
				t.Error("updated_at should be Omittable in UpdateInput")
			}
		}
	}
	if !foundUpdatedAt {
		t.Error("updated_at should be included in UpdateInput")
	}

	// Non-PK, non-computed fields remain: name, updated_at.
	if len(tc.UpdateInputFields) != 2 {
		t.Errorf("UpdateInputFields has %d entries, want 2 (name, updated_at)", len(tc.UpdateInputFields))
	}
}

// --- Operations: schema facts only (PRD §4.6) ---

// TestOperations_ResolvedSetIsTheSchemaFacts pins that a table's resolved
// client operations are decided by schema facts alone, since the client has no
// operations toggle. Every method is on, except the ones a missing schema fact
// takes away — and each row takes away exactly its own.
func TestOperations_ResolvedSetIsTheSchemaFacts(t *testing.T) {
	all := gen.ResolvedOperations{
		Get: true, GetMany: true, Create: true, CreateMany: true,
		Update: true, UpdateMany: true, UpdateWhere: true,
		Upsert: true, UpsertMany: true,
		SoftDelete: true, Restore: true, HardDelete: true,
		Exists: true, Count: true, Increment: true,
		Paginate: true, Connection: true, Stream: true,
	}
	without := func(clear func(*gen.ResolvedOperations)) gen.ResolvedOperations {
		ops := all
		clear(&ops)
		return ops
	}
	tests := []struct {
		name    string
		columns []parser.Column
		tables  map[string]config.TableConfig
		want    gen.ResolvedOperations
	}{
		{
			name: "every fact present",
			columns: []parser.Column{
				{Name: "id", Type: "bigserial", PrimaryKey: true, Default: "nextval('id_seq')"},
				{Name: "stock", Type: "integer"},
				{Name: "deleted_at", Type: "timestamptz", Nullable: true},
			},
			want: all,
		},
		{
			name: "no soft-delete column",
			columns: []parser.Column{
				{Name: "id", Type: "bigserial", PrimaryKey: true, Default: "nextval('id_seq')"},
				{Name: "stock", Type: "integer"},
			},
			want: without(func(o *gen.ResolvedOperations) { o.SoftDelete, o.Restore = false, false }),
		},
		{
			name: "no incrementable column",
			columns: []parser.Column{
				{Name: "id", Type: "bigserial", PrimaryKey: true, Default: "nextval('id_seq')"},
				{Name: "name", Type: "text"},
				{Name: "deleted_at", Type: "timestamptz", Nullable: true},
			},
			want: without(func(o *gen.ResolvedOperations) { o.Increment = false }),
		},
		{
			// An app-enforced key with no UNIQUE beside it emits no
			// conflict-target constant (PRD §9.5), so there is nothing to
			// pass Upsert.
			name: "no conflict target",
			columns: []parser.Column{
				{Name: "id", Type: "bigint"},
				{Name: "stock", Type: "integer"},
				{Name: "deleted_at", Type: "timestamptz", Nullable: true},
			},
			tables: map[string]config.TableConfig{
				"products": {PrimaryKey: &config.TablePrimaryKeyConfig{Columns: []string{"id"}}},
			},
			want: without(func(o *gen.ResolvedOperations) { o.Upsert, o.UpsertMany = false, false }),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := testInput(&parser.Schema{Tables: []parser.Table{{Name: "products", Columns: tt.columns}}})
			maps.Copy(input.Config.Tables, tt.tables)
			contexts, err := gen.BuildTableContexts(input, nil)
			if err != nil {
				t.Fatalf("BuildTableContexts() error: %v", err)
			}
			if len(contexts) != 1 {
				t.Fatalf("BuildTableContexts() returned %d contexts, want 1", len(contexts))
			}
			if diff := cmp.Diff(tt.want, contexts[0].Operations); diff != "" {
				t.Errorf("Operations mismatch (-want +got):\n%s", diff)
			}
		})
	}

	// A view's read client is the other entity kind §4.6 gates: no write
	// method at all, and no Connection when the inherited cursor keys name a
	// column the view lacks (it has no PK to fall back to).
	t.Run("view whose inherited cursor keys do not resolve", func(t *testing.T) {
		input := testInput(&parser.Schema{Views: []parser.View{{
			Name:    "product_totals",
			Columns: []parser.Column{{Name: "sku", Type: "text"}, {Name: "total", Type: "bigint"}},
		}}})
		views, err := gen.BuildViewContexts(input, nil)
		if err != nil {
			t.Fatalf("BuildViewContexts() error: %v", err)
		}
		if len(views) != 1 {
			t.Fatalf("BuildViewContexts() returned %d contexts, want 1", len(views))
		}
		if views[0].HasConnection || views[0].HasPK {
			t.Errorf("HasConnection, HasPK = %v, %v, want false, false", views[0].HasConnection, views[0].HasPK)
		}
	})
}

// --- ExcludeDeleted tests ---

func TestExcludeDeleted_defaultTrueWhenSoftDeletePresent(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "bigserial", PrimaryKey: true, Default: "nextval('id_seq')"},
					{Name: "name", Type: "text"},
					{Name: "deleted_at", Type: "timestamptz", Nullable: true},
				},
			},
		},
	}

	input := testInput(schema)

	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}

	tc := contexts[0]

	if tc.SoftDelete == nil {
		t.Fatal("SoftDelete should be detected")
	}
	if !tc.ExcludeDeleted {
		t.Error("ExcludeDeleted should default to true when SoftDelete is present")
	}
}

func TestExcludeDeleted_falseWhenNoSoftDelete(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "bigserial", PrimaryKey: true, Default: "nextval('id_seq')"},
					{Name: "name", Type: "text"},
				},
			},
		},
	}

	input := testInput(schema)

	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}

	tc := contexts[0]

	if tc.ExcludeDeleted {
		t.Error("ExcludeDeleted should be false when no SoftDelete column exists")
	}
}

func TestExcludeDeleted_respectsExplicitConfigOverride(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "bigserial", PrimaryKey: true, Default: "nextval('id_seq')"},
					{Name: "name", Type: "text"},
					{Name: "deleted_at", Type: "timestamptz", Nullable: true},
				},
			},
		},
	}

	input := testInput(schema)
	// Explicitly override to false even though soft delete is present.
	input.Config.Generation.ExcludeDeleted = new(false)

	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}

	tc := contexts[0]

	if tc.SoftDelete == nil {
		t.Fatal("SoftDelete should be detected")
	}
	if tc.ExcludeDeleted {
		t.Error("ExcludeDeleted should respect explicit config override (false)")
	}
}

// --- FilterFieldContext soft delete column test ---

func TestFilterFieldContext_softDeleteColumnMarked(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "bigserial", PrimaryKey: true, Default: "nextval('id_seq')"},
					{Name: "name", Type: "text"},
					{Name: "deleted_at", Type: "timestamptz", Nullable: true},
				},
			},
		},
	}

	input := testInput(schema)

	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}

	tc := contexts[0]

	var softDeleteCount int
	for _, f := range tc.FilterFields {
		if f.IsSoftDeleteColumn {
			softDeleteCount++
			if f.ColumnName != "deleted_at" {
				t.Errorf("IsSoftDeleteColumn on wrong column: %q, want %q", f.ColumnName, "deleted_at")
			}
		}
	}

	if softDeleteCount != 1 {
		t.Errorf("expected exactly 1 filter field with IsSoftDeleteColumn, got %d", softDeleteCount)
	}
}

func TestFilterFieldContext_noSoftDeleteColumn(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "bigserial", PrimaryKey: true, Default: "nextval('id_seq')"},
					{Name: "name", Type: "text"},
				},
			},
		},
	}

	input := testInput(schema)

	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}

	tc := contexts[0]

	for _, f := range tc.FilterFields {
		if f.IsSoftDeleteColumn {
			t.Errorf("no filter field should have IsSoftDeleteColumn when soft delete is not present, but %q does", f.ColumnName)
		}
	}
}

// --- InputFieldContext JSONTag test ---

func TestInputFieldContext_JSONTagPopulated(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "products",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "name", Type: "text"},
					{Name: "price", Type: "numeric"},
					{Name: "updated_at", Type: "timestamptz"},
					{Name: "deleted_at", Type: "timestamptz", Nullable: true},
				},
			},
		},
	}

	input := testInput(schema)

	contexts, err := gen.BuildTableContexts(input, nil)
	if err != nil {
		t.Fatalf("BuildTableContexts() error: %v", err)
	}

	tc := contexts[0]

	// CreateInputFields: all should have JSONTag == column name.
	for _, f := range tc.CreateInputFields {
		if f.JSONTag != f.ColumnName {
			t.Errorf("CreateInput field %q JSONTag = %q, want %q", f.ColumnName, f.JSONTag, f.ColumnName)
		}
	}

	// UpdateInputFields: all should have JSONTag == column name.
	for _, f := range tc.UpdateInputFields {
		if f.JSONTag != f.ColumnName {
			t.Errorf("UpdateInput field %q JSONTag = %q, want %q", f.ColumnName, f.JSONTag, f.ColumnName)
		}
	}
}

// --- Helpers ---

func dialectFor(name config.Dialect) sql.Dialect {
	switch name {
	case config.DialectPostgres:
		return sql.NewPostgresDialect()
	case config.DialectMySQL:
		return sql.NewMySQLDialect()
	case config.DialectSQLite:
		return sql.NewSQLiteDialect()
	default:
		return sql.NewPostgresDialect()
	}
}

func TestBuildViewContexts_Materialized(t *testing.T) {
	schema := &parser.Schema{
		Views: []parser.View{
			{
				Name:                    "order_totals",
				Schema:                  "public",
				Materialized:            true,
				ConcurrentlyRefreshable: true,
				Columns: []parser.Column{
					{Name: "customer_id", Type: "bigint", PrimaryKey: true},
					{Name: "total", Type: "numeric", Nullable: true},
				},
			},
			{
				Name:         "plain_totals",
				Schema:       "public",
				Materialized: true,
				Columns: []parser.Column{
					{Name: "customer_id", Type: "bigint"},
				},
			},
		},
	}

	contexts, err := gen.BuildViewContexts(testInput(schema), nil)
	if err != nil {
		t.Fatalf("BuildViewContexts() error: %v", err)
	}
	if len(contexts) != 2 {
		t.Fatalf("BuildViewContexts() returned %d contexts, want 2", len(contexts))
	}

	vc := contexts[0]
	if vc.ViewName != "order_totals" {
		t.Fatalf("contexts[0].ViewName = %q, want order_totals", vc.ViewName)
	}
	if !vc.Materialized {
		t.Error("order_totals Materialized = false, want true")
	}
	if !vc.ConcurrentlyRefreshable {
		t.Error("order_totals ConcurrentlyRefreshable = false, want true")
	}
	if !vc.HasPK {
		t.Error("order_totals HasPK = false, want true (discovered PK column)")
	}

	plain := contexts[1]
	if plain.ViewName != "plain_totals" {
		t.Fatalf("contexts[1].ViewName = %q, want plain_totals", plain.ViewName)
	}
	if !plain.Materialized {
		t.Error("plain_totals Materialized = false, want true")
	}
	if plain.ConcurrentlyRefreshable {
		t.Error("plain_totals ConcurrentlyRefreshable = true, want false")
	}
}

func TestBuildViewContexts_RegularViewUnchanged(t *testing.T) {
	schema := &parser.Schema{
		Views: []parser.View{
			{
				Name:   "product_summary",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint"},
				},
			},
		},
	}

	contexts, err := gen.BuildViewContexts(testInput(schema), nil)
	if err != nil {
		t.Fatalf("BuildViewContexts() error: %v", err)
	}
	if len(contexts) != 1 {
		t.Fatalf("BuildViewContexts() returned %d contexts, want 1", len(contexts))
	}
	if contexts[0].Materialized {
		t.Error("regular view Materialized = true, want false")
	}
	if contexts[0].ConcurrentlyRefreshable {
		t.Error("regular view ConcurrentlyRefreshable = true, want false")
	}
}

func TestBuildViewContexts_InvariantViolationPanics(t *testing.T) {
	schema := &parser.Schema{
		Views: []parser.View{
			{
				Name:                    "broken_fixture",
				Schema:                  "public",
				Materialized:            false,
				ConcurrentlyRefreshable: true,
				Columns: []parser.Column{
					{Name: "id", Type: "bigint"},
				},
			},
		},
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("BuildViewContexts() did not panic on !Materialized && ConcurrentlyRefreshable")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "ConcurrentlyRefreshable requires Materialized") {
			t.Errorf("panic = %v, want message mentioning the invariant", r)
		}
	}()
	_, _ = gen.BuildViewContexts(testInput(schema), nil)
}

// TestBuildViewContexts_SumAggregateWidening pins the end-to-end seam: a view
// column carrying Aggregate:"SUM" over an integer base resolves to the widened
// Go type (*int64 in PostgreSQL), not the un-widened *int32 the base column
// would give. Guards the parser.Column.Aggregate → context_view →
// ResolveAggregate wiring.
func TestBuildViewContexts_SumAggregateWidening(t *testing.T) {
	schema := &parser.Schema{
		Views: []parser.View{
			{
				Name:   "order_totals",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "product_id", Type: "uuid"},
					// SUM(integer) — set by resolveViewColumns for a flow-through aggregate.
					{Name: "total_quantity", Type: "integer", Nullable: true, Aggregate: "SUM"},
					// A plain integer column (no aggregate) stays narrow.
					{Name: "raw_qty", Type: "integer", Nullable: true},
				},
			},
		},
	}

	contexts, err := gen.BuildViewContexts(testInput(schema), nil)
	if err != nil {
		t.Fatalf("BuildViewContexts() error: %v", err)
	}
	if len(contexts) != 1 {
		t.Fatalf("BuildViewContexts() = %d contexts, want 1", len(contexts))
	}

	byName := make(map[string]gen.ColumnContext, len(contexts[0].Columns))
	for _, c := range contexts[0].Columns {
		byName[c.Name] = c
	}
	if got := byName["total_quantity"].GoType; got != "*int64" {
		t.Errorf("SUM(integer) column GoType = %q, want *int64 (widened to bigint)", got)
	}
	if got := byName["raw_qty"].GoType; got != "*int32" {
		t.Errorf("plain integer column GoType = %q, want *int32 (not widened)", got)
	}
}
