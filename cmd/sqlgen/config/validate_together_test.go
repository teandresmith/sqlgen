package config_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// PRD §4.13: all validation errors are reported together, not one at a time.
// Each case triggers one rule beside a fixed second error, and both must come
// back from the same validation run.

// TestValidatePreParse_ReportsEachRuleWithTheOthers loads each rule's YAML
// beside a pgx driver on SQLite, the driver/dialect mismatch.
func TestValidatePreParse_ReportsEachRuleWithTheOthers(t *testing.T) {
	const base = "input:\n  dialect: sqlite\n  paths: [\"./migrations\"]\noutput:\n  dir: ./models\n  driver: pgx\n"
	const partner = "output.driver: pgx is only compatible with input.dialect: postgres"

	tests := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "view api mutation operation",
			yaml: "api:\n  enabled: true\n  graphql:\n    enabled: true\nviews:\n  product_summary:\n    sql: ./views/product_summary.sql\n    api:\n      operations:\n        create: true\n",
			want: "views.product_summary.api.operations.create",
		},
		{
			name: "view api opt-in under a global opt-out",
			yaml: "api:\n  enabled: false\nviews:\n  product_summary:\n    sql: ./views/product_summary.sql\n    api:\n      enabled: true\n",
			want: "views.product_summary.api.enabled",
		},
		{
			name: "relationship with both filter and discriminator",
			yaml: "tables:\n  assets:\n    relationships:\n      - name: Attachments\n        type: o2m\n        table: documents\n        fk: entity_id\n        filter: \"entity_type = 'asset'\"\n        discriminator:\n          column: entity_type\n          value: asset\n",
			want: "`filter` and `discriminator` are mutually exclusive",
		},
		{
			name: "sort on an o2o relationship",
			yaml: "tables:\n  assets:\n    relationships:\n      - name: Owner\n        type: o2o\n        table: owners\n        fk: owner_id\n        sort:\n          - column: name\n",
			want: `("Owner").sort: an o2o relationship is loaded by a LEFT JOIN`,
		},
		{
			name: "relationship missing its link key",
			yaml: "tables:\n  owners:\n    relationships:\n      - name: Profiles\n        type: o2m\n        table: owner_profiles\n",
			want: "fk: required on an o2o or o2m relationship",
		},
		{
			name: "nested mutations max depth other than one",
			yaml: "generation:\n  nested_mutations:\n    max_depth: 2\n",
			want: "generation.nested_mutations.max_depth",
		},
		{
			name: "unknown nested mutation verb",
			yaml: "generation:\n  nested_mutations:\n    verbs: [create, attach]\n",
			want: "generation.nested_mutations.verbs[1]",
		},
		{
			name: "with-related key enabled without its base",
			yaml: "api:\n  enabled: true\n  graphql:\n    enabled: true\n  operations:\n    create: false\n    create_with_related: true\n",
			want: "create_with_related",
		},
		{
			name: "invalid api operations preset",
			yaml: "api:\n  enabled: true\n  graphql:\n    enabled: true\n  operations: write_only\n",
			want: "api.operations.preset",
		},
		{
			name: "client operations key under generation",
			yaml: "generation:\n  operations: read_only\n",
			want: "generation.operations: the Go client generates every method the schema allows",
		},
		{
			name: "client-only key in an api operations mask",
			yaml: "api:\n  enabled: true\n  graphql:\n    enabled: true\n  operations:\n    count: true\n",
			want: "not an API operation",
		},
		{
			name: "manifest enabled with no formats",
			yaml: "generation:\n  manifest:\n    enabled: true\n    formats: []\n",
			want: "manifest.formats: at least one format",
		},
		{
			name: "unknown manifest format",
			yaml: "generation:\n  manifest:\n    enabled: true\n    formats: [json, xml]\n",
			want: `manifest.formats: unknown value "xml"`,
		},
		{
			name: "manifest json layout outside its closed set",
			yaml: "generation:\n  manifest:\n    enabled: true\n    json_layout: nested\n",
			want: `manifest.json_layout: "nested" is not one of single, per_entity`,
		},
		{
			name: "manifest markdown dir escaping output dir",
			yaml: "generation:\n  manifest:\n    enabled: true\n    markdown_dir: ../docs\n",
			want: `manifest.markdown_dir: must be a subdirectory of its parent, got "../docs"`,
		},
		{
			name: "manifest per-entity dir escaping markdown dir",
			yaml: "generation:\n  manifest:\n    enabled: true\n    json_layout: per_entity\n    json_per_entity_dir: ../entities\n",
			want: `manifest.json_per_entity_dir: must be a subdirectory of its parent, got "../entities"`,
		},
		{
			name: "manifest json filename with a path separator",
			yaml: "generation:\n  manifest:\n    enabled: true\n    json_filename: sub/manifest.json\n",
			want: "manifest.json_filename: must be a bare filename",
		},
		{
			name: "table manifest opt-in under a global opt-out",
			yaml: "tables:\n  users:\n    manifest:\n      enabled: true\n",
			want: "tables.users.manifest.enabled",
		},
		{
			name: "manifest breadcrumb with the manifest off",
			yaml: "generation:\n  manifest:\n    enabled: false\n    breadcrumbs:\n      claude_md: true\n",
			want: "manifest.breadcrumbs.claude_md",
		},
		{
			name: "manifest embed with the manifest off",
			yaml: "generation:\n  manifest:\n    enabled: false\n    embed_in_client: true\n",
			want: "manifest.embed_in_client",
		},
		{
			name: "manifest mcp emit with the manifest off",
			yaml: "generation:\n  manifest:\n    enabled: false\n    mcp:\n      emit_project_config: true\n",
			want: "manifest.mcp.emit_project_config",
		},
		{
			name: "manifest mcp project config escaping the module root",
			yaml: "generation:\n  manifest:\n    enabled: true\n    mcp:\n      project_configs: [../.mcp.json]\n",
			want: "manifest.mcp.project_configs",
		},
		{
			name: "column name override that is not a Go identifier",
			yaml: "tables:\n  users:\n    column_map:\n      api_key:\n        name: Api Key\n",
			want: "is not a valid Go identifier",
		},
		{
			name: "column name override on an excluded column",
			yaml: "tables:\n  users:\n    exclude_columns: [api_key]\n    column_map:\n      api_key:\n        name: APIKey\n",
			want: "tables.users.column_map.api_key.name",
		},
		{
			name: "column type override that is not a Go type",
			yaml: "tables:\n  users:\n    column_map:\n      balance:\n        type: \"42\"\n",
			want: "is not a Go type expression",
		},
		{
			name: "column type override on an excluded column",
			yaml: "tables:\n  users:\n    exclude_columns: [balance]\n    column_map:\n      balance:\n        type: int64\n",
			want: "tables.users.column_map.balance.type",
		},
		{
			name: "column import without a type",
			yaml: "tables:\n  users:\n    column_map:\n      balance:\n        import: github.com/shopspring/decimal\n",
			want: "is set without a type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := config.LoadConfig(writeTestConfig(t, base+tt.yaml))
			if err != nil {
				t.Fatalf("LoadConfig() error = %v, want the config to decode", err)
			}
			_, err = config.ValidatePreParse(cfg)
			if err == nil {
				t.Fatalf("ValidatePreParse() = nil, want %q and %q", tt.want, partner)
			}
			for _, want := range []string{tt.want, partner} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("ValidatePreParse() error = %v, want it to contain %q", err, want)
				}
			}
		})
	}
}

// TestValidatePostParse_ReportsEachRuleWithTheOthers runs each schema-level
// rule beside an explicit cursor_keys entry naming a column the partner table
// does not have.
func TestValidatePostParse_ReportsEachRuleWithTheOthers(t *testing.T) {
	const partner = `cursor_keys: column "nonexistent" does not exist in table public.partners`
	partnerTable := config.SchemaTable{
		Name: "partners", Schema: "public",
		Columns: []config.SchemaColumn{{Name: "id", Type: "bigint", PrimaryKey: true}},
	}
	users := func(extra ...config.SchemaColumn) config.SchemaTable {
		return config.SchemaTable{
			Name: "users", Schema: "public",
			Columns: append([]config.SchemaColumn{
				{Name: "id", Type: "bigint", PrimaryKey: true, AutoIncrement: true},
				{Name: "email", Type: "text"},
				{Name: "handle", Type: "text"},
				{Name: "nickname", Type: "text", Nullable: true},
			}, extra...),
		}
	}
	withAPI := func(cfg *config.RootConfig) {
		cfg.API = &config.APIConfig{Enabled: true, GraphQL: &config.GraphQLAPIConfig{Enabled: true}}
	}

	tests := []struct {
		name   string
		mutate func(*config.RootConfig)
		tables []config.SchemaTable
		views  []config.SchemaView
		want   string
	}{
		{
			name: "explicit view cursor key missing from the view",
			mutate: func(cfg *config.RootConfig) {
				cfg.Views["product_summary"] = config.ViewConfig{SQL: "views/product_summary.sql", CursorKeys: []string{"missing_col"}}
			},
			views: []config.SchemaView{{
				Name: "product_summary", Schema: "public",
				Columns: []config.SchemaColumn{{Name: "id", Type: "bigint"}},
			}},
			want: `cursor_keys: column "missing_col" does not exist in view public.product_summary`,
		},
		{
			name: "explicit cursor key naming a nullable column",
			mutate: func(cfg *config.RootConfig) {
				cfg.Tables["users"] = config.TableConfig{CursorKeys: []string{"nickname"}}
			},
			tables: []config.SchemaTable{users()},
			want:   `cursor_keys: column "nickname" in table public.users is nullable`,
		},
		{
			name: "cursor key with a restricted access role",
			mutate: func(cfg *config.RootConfig) {
				cfg.Tables["users"] = config.TableConfig{
					CursorKeys: []string{"handle"},
					ColumnMap:  map[string]config.ColumnOverride{"handle": {Access: "hidden"}},
				}
			},
			tables: []config.SchemaTable{users()},
			want:   "is not allowed on a cursor_keys column",
		},
		{
			name: "primary key override naming a missing column",
			mutate: func(cfg *config.RootConfig) {
				cfg.Tables["users"] = config.TableConfig{PrimaryKey: &config.TablePrimaryKeyConfig{Columns: []string{"ghost"}}}
			},
			tables: []config.SchemaTable{users()},
			want:   `column "ghost" does not exist`,
		},
		{
			name:   "exclude_columns removing a primary key column",
			mutate: func(cfg *config.RootConfig) { cfg.Tables["users"] = config.TableConfig{ExcludeColumns: []string{"id"}} },
			tables: []config.SchemaTable{users()},
			want:   `exclude_columns removes primary-key column "id" of table public.users`,
		},
		{
			name: "primary key override naming a nullable column",
			mutate: func(cfg *config.RootConfig) {
				cfg.Tables["users"] = config.TableConfig{PrimaryKey: &config.TablePrimaryKeyConfig{Columns: []string{"nickname"}}}
			},
			tables: []config.SchemaTable{users()},
			want:   `primary_key.columns: column "nickname" is nullable; primary key columns must be NOT NULL`,
		},
		{
			name: "db strategy on a composite key",
			mutate: func(cfg *config.RootConfig) {
				cfg.Tables["timeline_events"] = config.TableConfig{PrimaryKey: &config.TablePrimaryKeyConfig{Strategy: config.PKStrategyDB}}
			},
			tables: []config.SchemaTable{{
				Name: "timeline_events", Schema: "public",
				Columns: []config.SchemaColumn{
					{Name: "id", Type: "bigint", PrimaryKey: true, AutoIncrement: true},
					{Name: "created_at", Type: "timestamptz", PrimaryKey: true, HasDefault: true},
				},
			}},
			want: `"db" is not supported on a composite primary key`,
		},
		{
			name: "NOT NULL timestamp soft delete column",
			mutate: func(cfg *config.RootConfig) {
				cfg.Generation.SoftDeleteColumns = []config.SoftDeleteConfig{{Name: "deleted_at", Type: config.SoftDeleteTimestamp}}
			},
			tables: []config.SchemaTable{users(config.SchemaColumn{Name: "deleted_at", Type: "timestamp"})},
			want:   `column "deleted_at" in table public.users is NOT NULL`,
		},
		{
			name: "nullable bool soft delete column with no default",
			mutate: func(cfg *config.RootConfig) {
				cfg.Generation.SoftDeleteColumns = []config.SoftDeleteConfig{{Name: "is_deleted", Type: config.SoftDeleteBool}}
			},
			tables: []config.SchemaTable{users(config.SchemaColumn{Name: "is_deleted", Type: "boolean", Nullable: true})},
			want:   `column "is_deleted" in table public.users is nullable with no DEFAULT`,
		},
		{
			name: "discriminator column missing from the related table",
			mutate: func(cfg *config.RootConfig) {
				rel := discriminatorRel("Attachments", "asset.attachment")
				rel.Discriminator.Column = "entity_kind"
				cfg.Tables["assets"] = config.TableConfig{Relationships: []config.TableRelationship{rel}}
			},
			tables: documentsSchema(),
			want:   "discriminator.column",
		},
		{
			name: "sort column missing from the related table",
			mutate: func(cfg *config.RootConfig) {
				cfg.Tables["assets"] = config.TableConfig{Relationships: []config.TableRelationship{
					sortRel("o2m", config.RelationshipSort{Column: "created_at"}),
				}}
			},
			tables: documentsSchema(),
			want:   ".sort[0].column",
		},
		{
			name: "relationship fk on neither table",
			mutate: func(cfg *config.RootConfig) {
				cfg.Tables["assets"] = config.TableConfig{Relationships: []config.TableRelationship{
					{Name: "Documents", Type: "o2m", Table: "documents", FK: "ghost_id"},
				}}
			},
			tables: documentsSchema(),
			want:   `.fk: "ghost_id"`,
		},
		{
			name: "restricted access role on a primary key column",
			mutate: func(cfg *config.RootConfig) {
				cfg.Tables["users"] = config.TableConfig{ColumnMap: map[string]config.ColumnOverride{"id": {Access: "hidden"}}}
			},
			tables: []config.SchemaTable{users()},
			want:   "PK columns must stay API-readable",
		},
		{
			name: "access role dropping a required-on-create column",
			mutate: func(cfg *config.RootConfig) {
				withAPI(cfg)
				cfg.Tables["users"] = config.TableConfig{ColumnMap: map[string]config.ColumnOverride{"email": {Access: "internal"}}}
			},
			tables: []config.SchemaTable{users()},
			want:   "required on create",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Tables["partners"] = config.TableConfig{CursorKeys: []string{"nonexistent"}}
			tt.mutate(cfg)
			tables := append([]config.SchemaTable{partnerTable}, tt.tables...)

			_, err := config.ValidatePostParse(cfg, tables, tt.views)
			if err == nil {
				t.Fatalf("ValidatePostParse() = nil, want %q and %q", tt.want, partner)
			}
			for _, want := range []string{tt.want, partner} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("ValidatePostParse() error = %v, want it to contain %q", err, want)
				}
			}
		})
	}
}
