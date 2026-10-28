package config_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// tgfBaseConfig is the smallest config LoadConfig and ValidatePreParse accept;
// each test appends the blocks it exercises.
const tgfBaseConfig = `
input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models
`

// tgfLoad writes tgfBaseConfig plus extra and loads it.
func tgfLoad(t *testing.T, extra string) *config.RootConfig {
	t.Helper()
	cfg, err := config.LoadConfig(writeTestConfig(t, tgfBaseConfig+extra))
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}
	return cfg
}

// tgfPreParseErrors returns the individual error lines ValidatePreParse
// reports for cfg.
func tgfPreParseErrors(cfg *config.RootConfig) []string {
	_, err := config.ValidatePreParse(cfg)
	if err == nil {
		return nil
	}
	return strings.Split(err.Error(), "\n")
}

// TestLoadConfig_ExplicitSchemaDetectionLists pins that an explicit
// soft_delete_columns / update_columns list replaces the built-in default
// list rather than merging with it.
func TestLoadConfig_ExplicitSchemaDetectionLists(t *testing.T) {
	cfg := tgfLoad(t, `
generation:
  soft_delete_columns:
    - name: archived_at
      type: timestamp
    - name: is_archived
      type: bool
  update_columns: [touched_at]
`)
	wantSoft := []config.SoftDeleteConfig{
		{Name: "archived_at", Type: "timestamp"},
		{Name: "is_archived", Type: "bool"},
	}
	if diff := cmp.Diff(wantSoft, cfg.Generation.SoftDeleteColumns); diff != "" {
		t.Errorf("Generation.SoftDeleteColumns mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"touched_at"}, cfg.Generation.UpdateColumns); diff != "" {
		t.Errorf("Generation.UpdateColumns mismatch (-want +got):\n%s", diff)
	}
}

// TestLoadConfig_ExcludeColumnsDefaultsEmpty pins the exclude_columns
// default: with the key unset, no column is excluded, globally or for a
// table that sets no list of its own.
func TestLoadConfig_ExcludeColumnsDefaultsEmpty(t *testing.T) {
	cfg := tgfLoad(t, `
tables:
  products: {}
`)
	if len(cfg.Generation.ExcludeColumns) != 0 {
		t.Errorf("Generation.ExcludeColumns = %q, want [] with the key unset", cfg.Generation.ExcludeColumns)
	}
	if got := config.ResolveTableExcludeColumns(cfg.Tables["products"], cfg.Generation); len(got) != 0 {
		t.Errorf("ResolveTableExcludeColumns(products) = %q, want []", got)
	}
}

// TestValidatePreParse_NestedMutationsMaxDepthDefault pins that an enabled
// nested_mutations block with max_depth unset validates: the key stays unset
// (not 0, which validation rejects), the same as an explicit max_depth: 1.
// Nothing resolves a depth from the key, since 1 is the only one v1
// implements, so this cannot show the depth itself.
func TestValidatePreParse_NestedMutationsMaxDepthDefault(t *testing.T) {
	tests := []struct {
		name  string
		block string
	}{
		{name: "max_depth unset", block: "    enabled: true\n"},
		{name: "max_depth 1 spelled out", block: "    enabled: true\n    max_depth: 1\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tgfLoad(t, "generation:\n  nested_mutations:\n"+tt.block)
			nm := cfg.Generation.NestedMutations
			if nm == nil {
				t.Fatal("Generation.NestedMutations = nil, want the loaded block")
			}
			if nm.MaxDepth != nil && *nm.MaxDepth != 1 {
				t.Errorf("NestedMutations.MaxDepth = %d, want 1 or unset", *nm.MaxDepth)
			}
			if errs := tgfPreParseErrors(cfg); errs != nil {
				t.Errorf("ValidatePreParse() errors = %q, want none: max_depth unset must mean depth 1", errs)
			}
		})
	}
}

// TestResolveTablePageSize_TableWins pins the resolution order for page_size:
// a table-level value beats the global one, which beats the built-in 100.
func TestResolveTablePageSize_TableWins(t *testing.T) {
	cfg := tgfLoad(t, `
generation:
  page_size: 40
tables:
  products:
    page_size: 15
`)
	if got := config.ResolveTablePageSize(cfg.Tables["products"], cfg.Generation); got != 15 {
		t.Errorf("ResolveTablePageSize(products) = %d, want 15 (tables.products.page_size over generation.page_size 40)", got)
	}
}

// TestResolveTableConnection_TableCursorKeysWin pins that a table's own
// cursor_keys beat generation.cursor_keys for that table, even when the
// global keys resolve on it too, while a table without its own inherits the
// global keys.
func TestResolveTableConnection_TableCursorKeysWin(t *testing.T) {
	cfg := tgfLoad(t, `
generation:
  cursor_keys: [created_at, id]
tables:
  products:
    cursor_keys: [name]
  orders: {}
`)
	cols := []config.SchemaColumn{{Name: "id"}, {Name: "created_at"}, {Name: "name"}}
	tests := []struct {
		table string
		want  []string
	}{
		{table: "products", want: []string{"name"}},
		{table: "orders", want: []string{"created_at", "id"}},
	}
	for _, tt := range tests {
		t.Run(tt.table, func(t *testing.T) {
			got := config.ResolveTableConnection(cfg.Tables[tt.table], cfg.Generation, "public."+tt.table, cols, []string{"id"})
			if diff := cmp.Diff(tt.want, got.Keys); diff != "" {
				t.Errorf("ResolveTableConnection(%s) keys mismatch (-want +got):\n%s", tt.table, diff)
			}
			if !got.Emit {
				t.Errorf("ResolveTableConnection(%s) Emit = false, want true (errs %v, warning %q)", tt.table, got.Errs, got.Warning)
			}
		})
	}
}

// TestResolveViewConnection_InheritsGlobalCursorKeys pins that a view with no
// cursor_keys of its own takes generation.cursor_keys, not the built-in
// ["id"].
func TestResolveViewConnection_InheritsGlobalCursorKeys(t *testing.T) {
	cfg := tgfLoad(t, `
generation:
  cursor_keys: [region, code]
views:
  sales_summary: {}
`)
	cols := []config.SchemaColumn{{Name: "id"}, {Name: "region"}, {Name: "code"}}
	got := config.ResolveViewConnection(cfg.Views["sales_summary"], cfg.Generation, "public.sales_summary", cols)
	if diff := cmp.Diff([]string{"region", "code"}, got.Keys); diff != "" {
		t.Errorf("ResolveViewConnection(sales_summary) keys mismatch (-want +got):\n%s", diff)
	}
	if !got.Emit {
		t.Errorf("ResolveViewConnection(sales_summary) Emit = false, want true (warning %q)", got.Warning)
	}
}

// TestLoadConfig_OverridesTypesDefaultsEmpty pins the overrides.types default:
// with the key unset the map is empty, so no SQL type is overridden.
func TestLoadConfig_OverridesTypesDefaultsEmpty(t *testing.T) {
	cfg := tgfLoad(t, "")
	if len(cfg.Overrides.Types) != 0 {
		t.Errorf("Overrides.Types = %+v, want {} with the key unset", cfg.Overrides.Types)
	}
}

// TestLoadConfig_OverridesTypesImportKept pins that an explicit
// overrides.types.<t>.import is kept as written.
func TestLoadConfig_OverridesTypesImportKept(t *testing.T) {
	cfg := tgfLoad(t, `
overrides:
  types:
    varchar:
      type: ksuid.KSUID
      import: github.com/segmentio/ksuid
`)
	if got := cfg.Overrides.Types["varchar"].Import; got != "github.com/segmentio/ksuid" {
		t.Errorf("Overrides.Types[varchar].Import = %q, want %q", got, "github.com/segmentio/ksuid")
	}
}

// TestTableOverrides_SameStructureAsGlobal pins that tables.<t>.overrides has
// the structure of the global overrides block: the same TypeOverride entry,
// full nullable form included, loads identically under both, and the JSON
// Schema types both maps' values with the one typeOverride definition.
func TestTableOverrides_SameStructureAsGlobal(t *testing.T) {
	const entry = `
      numeric:
        type: decimal.Decimal
        import: github.com/shopspring/decimal
        zero_value: decimal.Zero
        nullable:
          type: decimal.NullDecimal
          import: github.com/shopspring/decimal
          underlying_field: Decimal
          valid_field: Valid
`
	cfg := tgfLoad(t, "overrides:\n  types:"+strings.ReplaceAll(entry, "\n      ", "\n    ")+
		"tables:\n  products:\n    overrides:\n      types:"+strings.ReplaceAll(entry, "\n      ", "\n        "))
	global, ok := cfg.Overrides.Types["numeric"]
	if !ok {
		t.Fatal("overrides.types.numeric not loaded")
	}
	tableOverrides := cfg.Tables["products"].Overrides
	if tableOverrides == nil {
		t.Fatal("tables.products.overrides not loaded")
	}
	if diff := cmp.Diff(global, tableOverrides.Types["numeric"]); diff != "" {
		t.Errorf("tables.products.overrides.types.numeric differs from overrides.types.numeric (-global +table):\n%s", diff)
	}
	if errs := tgfPreParseErrors(cfg); errs != nil {
		t.Errorf("ValidatePreParse() errors = %q, want none", errs)
	}

	globalRef := tgfSchemaTypesRef(t, "overrides")
	tableRef := tgfSchemaTypesRef(t, "tableOverrides")
	if globalRef != "#/$defs/typeOverride" || tableRef != globalRef {
		t.Errorf("schema overrides.types values = %q, tables.<t>.overrides.types values = %q, want both %q", globalRef, tableRef, "#/$defs/typeOverride")
	}
}

// TestValidatePreParse_RelationshipWithoutTableRejected pins that a relationship
// with no `table` is a config error naming the relationship — its index, and
// its name when it has one — and the key.
func TestValidatePreParse_RelationshipWithoutTableRejected(t *testing.T) {
	tests := []struct {
		name  string
		entry string
		want  string
	}{
		{
			name:  "named relationship",
			entry: "      - name: Attachments\n        type: o2m\n        fk: product_id\n",
			want:  `tables.products.relationships[0] ("Attachments").table: required — it names the related table the relationship links to (PRD §4.8)`,
		},
		{
			name:  "unnamed relationship",
			entry: "      - type: o2m\n        fk: product_id\n",
			want:  "tables.products.relationships[0].table: required — it names the related table the relationship links to (PRD §4.8)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tgfLoad(t, "tables:\n  products:\n    relationships:\n"+tt.entry)
			errs := tgfPreParseErrors(cfg)
			if !slices.Contains(errs, tt.want) {
				t.Errorf("ValidatePreParse() errors = %q, want them to include %q", errs, tt.want)
			}
		})
	}
}

// TestValidatePreParse_ViewStructNameOptional pins that a view entry without
// struct_name is valid.
func TestValidatePreParse_ViewStructNameOptional(t *testing.T) {
	cfg := tgfLoad(t, `
views:
  sales_summaries:
    cursor_keys: [region]
`)
	if got := cfg.Views["sales_summaries"].StructName; got != "" {
		t.Errorf("Views[sales_summaries].StructName = %q, want it left unset", got)
	}
	if errs := tgfPreParseErrors(cfg); errs != nil {
		t.Errorf("ValidatePreParse() errors = %q, want none for a view entry without struct_name", errs)
	}
}

// TestValidatePreParse_CachedViewRequiresInvalidateOn pins that a cached view
// entry needs a non-empty invalidate_on. The rule reads only the views.<v>
// entry; the parsed-schema counterpart, which runs it on a CREATE VIEW and a
// CREATE MATERIALIZED VIEW, is cli's TestValidate_CachedViewRequiresInvalidateOn.
func TestValidatePreParse_CachedViewRequiresInvalidateOn(t *testing.T) {
	cfg := tgfLoad(t, "views:\n  sales_summary:\n    cache:\n      enabled: true\n")
	const want = "views.sales_summary.cache.enabled is true but invalidate_on is empty; views must declare at least one source table (TTL-only view caches are not supported)"
	if diff := cmp.Diff([]string{want}, tgfPreParseErrors(cfg)); diff != "" {
		t.Errorf("ValidatePreParse() errors mismatch (-want +got):\n%s", diff)
	}
}

// TestViewConfig_NoMaterializedOrRefreshKey pins that materialized-ness is not
// a config toggle: views.<v>.materialized and views.<v>.refresh are unknown
// keys to the loader and are not declared by the JSON Schema.
func TestViewConfig_NoMaterializedOrRefreshKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
		line string
	}{
		{name: "materialized key", key: "materialized", line: "materialized: true"},
		{name: "refresh key", key: "refresh", line: "refresh: concurrently"},
	}
	var schema struct {
		Defs map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(config.SchemaV1(), &schema); err != nil {
		t.Fatalf("decoding the JSON Schema: %v", err)
	}
	viewProps := schema.Defs["view"].Properties
	if len(viewProps) == 0 {
		t.Fatal("JSON Schema has no $defs.view properties to check")
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.LoadConfig(writeTestConfig(t, tgfBaseConfig+"views:\n  sales_rollup_mv:\n    "+tt.line+"\n"))
			want := "field " + tt.key + " not found in type config.ViewConfig"
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("LoadConfig() error = %v, want an unknown-field error containing %q", err, want)
			}
			if _, ok := viewProps[tt.key]; ok {
				t.Errorf("JSON Schema $defs.view declares %q, want no such property", tt.key)
			}
		})
	}
}

// tgfSchemaTypesRef returns the $ref the JSON Schema gives the values of
// $defs.<def>.properties.types.
func tgfSchemaTypesRef(t *testing.T, def string) string {
	t.Helper()
	var raw struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(config.SchemaV1(), &raw); err != nil {
		t.Fatalf("decoding the JSON Schema: %v", err)
	}
	body, ok := raw.Defs[def]
	if !ok {
		t.Fatalf("JSON Schema has no $defs.%s", def)
	}
	var entry struct {
		Properties struct {
			Types struct {
				AdditionalProperties struct {
					Ref string `json:"$ref"`
				} `json:"additionalProperties"`
			} `json:"types"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(body, &entry); err != nil {
		t.Fatalf("decoding $defs.%s: %v", def, err)
	}
	return entry.Properties.Types.AdditionalProperties.Ref
}
