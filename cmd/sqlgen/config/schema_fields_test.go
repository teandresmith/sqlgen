package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// TestSchemaV1_fieldValueTypes pins the value type v1.json declares at each
// config path. Every case pairs a value of the declared type, which must
// validate, with a value of another type at the same path, which must be
// rejected by that path's own keyword: the error has to land on the key, not
// somewhere else in the document.
func TestSchemaV1_fieldValueTypes(t *testing.T) {
	sch := compileSchema(t)

	tests := []struct {
		name    string
		good    string
		bad     string
		at      string // JSON pointer of the rejected instance
		keyword string // last segment of the rejecting keyword location
	}{
		{
			name:    "version given an integer",
			good:    "version: v1\n",
			bad:     "version: 1\n",
			at:      "/version",
			keyword: "type",
		},
		{
			name:    "enums given an array",
			good:    "enums:\n  status:\n    struct_name: Status\n",
			bad:     "enums: [status]\n",
			at:      "/enums",
			keyword: "type",
		},
		{
			name:    "enums entry given a string",
			good:    "enums:\n  status:\n    struct_name: Status\n",
			bad:     "enums:\n  status: Status\n",
			at:      "/enums/status",
			keyword: "type",
		},
		{
			name:    "enums entry given an unknown key",
			good:    "enums:\n  status:\n    description: Order status.\n",
			bad:     "enums:\n  status:\n    values: [a, b]\n",
			at:      "/enums/status",
			keyword: "additionalProperties",
		},
		{
			name:    "tables given an array",
			good:    "tables:\n  users:\n    struct_name: Account\n",
			bad:     "tables: [users]\n",
			at:      "/tables",
			keyword: "type",
		},
		{
			name:    "tables entry given a string",
			good:    "tables:\n  users:\n    struct_name: Account\n",
			bad:     "tables:\n  users: Account\n",
			at:      "/tables/users",
			keyword: "type",
		},
		{
			name:    "tables entry given an unknown key",
			good:    "tables:\n  users:\n    struct_name: Account\n",
			bad:     "tables:\n  users:\n    materialized: true\n",
			at:      "/tables/users",
			keyword: "additionalProperties",
		},
		{
			name:    "views given an array",
			good:    "views:\n  sales_summary:\n    struct_name: SalesSummary\n",
			bad:     "views: [sales_summary]\n",
			at:      "/views",
			keyword: "type",
		},
		{
			name:    "views entry given a string",
			good:    "views:\n  sales_summary:\n    struct_name: SalesSummary\n",
			bad:     "views:\n  sales_summary: SalesSummary\n",
			at:      "/views/sales_summary",
			keyword: "type",
		},
		{
			name:    "views entry given an unknown key",
			good:    "views:\n  sales_summary:\n    struct_name: SalesSummary\n",
			bad:     "views:\n  sales_summary:\n    materialized: true\n",
			at:      "/views/sales_summary",
			keyword: "additionalProperties",
		},
		{
			name:    "input.source given an array",
			good:    "input:\n  source: files\n",
			bad:     "input:\n  source: [files]\n",
			at:      "/input/source",
			keyword: "type",
		},
		{
			name:    "input.schema given an integer",
			good:    "input:\n  schema: public\n",
			bad:     "input:\n  schema: 1\n",
			at:      "/input/schema",
			keyword: "type",
		},
		{
			name:    "input.paths given a string",
			good:    "input:\n  paths: [./migrations]\n",
			bad:     "input:\n  paths: ./migrations\n",
			at:      "/input/paths",
			keyword: "type",
		},
		{
			name:    "input.paths item given an integer",
			good:    "input:\n  paths: [./migrations]\n",
			bad:     "input:\n  paths: [1]\n",
			at:      "/input/paths/0",
			keyword: "type",
		},
		{
			name:    "input.views given a string",
			good:    "input:\n  views: [./views]\n",
			bad:     "input:\n  views: ./views\n",
			at:      "/input/views",
			keyword: "type",
		},
		{
			name:    "input.views item given a boolean",
			good:    "input:\n  views: [./views]\n",
			bad:     "input:\n  views: [true]\n",
			at:      "/input/views/0",
			keyword: "type",
		},
		{
			name:    "input.introspect.schemas given a string",
			good:    "input:\n  introspect:\n    schemas: [public]\n",
			bad:     "input:\n  introspect:\n    schemas: public\n",
			at:      "/input/introspect/schemas",
			keyword: "type",
		},
		{
			name:    "input.introspect.schemas item given an integer",
			good:    "input:\n  introspect:\n    schemas: [public]\n",
			bad:     "input:\n  introspect:\n    schemas: [1]\n",
			at:      "/input/introspect/schemas/0",
			keyword: "type",
		},
		{
			name:    "input.introspect.exclude_schemas given a string",
			good:    "input:\n  introspect:\n    exclude_schemas: [audit]\n",
			bad:     "input:\n  introspect:\n    exclude_schemas: audit\n",
			at:      "/input/introspect/exclude_schemas",
			keyword: "type",
		},
		{
			name:    "input.introspect.exclude_schemas item given an integer",
			good:    "input:\n  introspect:\n    exclude_schemas: [audit]\n",
			bad:     "input:\n  introspect:\n    exclude_schemas: [1]\n",
			at:      "/input/introspect/exclude_schemas/0",
			keyword: "type",
		},
		{
			name:    "input.introspect.tables given a string",
			good:    "input:\n  introspect:\n    tables: [users]\n",
			bad:     "input:\n  introspect:\n    tables: users\n",
			at:      "/input/introspect/tables",
			keyword: "type",
		},
		{
			name:    "input.introspect.tables item given an integer",
			good:    "input:\n  introspect:\n    tables: [users]\n",
			bad:     "input:\n  introspect:\n    tables: [1]\n",
			at:      "/input/introspect/tables/0",
			keyword: "type",
		},
		{
			name:    "exclude_tables given a string",
			good:    "exclude_tables: [\"tmp_*\"]\n",
			bad:     "exclude_tables: \"tmp_*\"\n",
			at:      "/exclude_tables",
			keyword: "type",
		},
		{
			name:    "exclude_tables item given an integer",
			good:    "exclude_tables: [\"tmp_*\"]\n",
			bad:     "exclude_tables: [1]\n",
			at:      "/exclude_tables/0",
			keyword: "type",
		},
		{
			name:    "output.dir given an integer",
			good:    "output:\n  dir: ./internal/models\n",
			bad:     "output:\n  dir: 1\n",
			at:      "/output/dir",
			keyword: "type",
		},
		{
			name:    "output.package given a boolean",
			good:    "output:\n  package: models\n",
			bad:     "output:\n  package: true\n",
			at:      "/output/package",
			keyword: "type",
		},
		{
			name:    "output.layout given an integer",
			good:    "output:\n  layout: single_file\n",
			bad:     "output:\n  layout: 1\n",
			at:      "/output/layout",
			keyword: "type",
		},
		{
			name:    "output.enums.file given an integer",
			good:    "output:\n  enums:\n    file: enums_gen.go\n",
			bad:     "output:\n  enums:\n    file: 1\n",
			at:      "/output/enums/file",
			keyword: "type",
		},
		{
			name:    "output.enums.package given a boolean",
			good:    "output:\n  enums:\n    package: enums\n",
			bad:     "output:\n  enums:\n    package: true\n",
			at:      "/output/enums/package",
			keyword: "type",
		},
		{
			name:    "output.types.file given an integer",
			good:    "output:\n  types:\n    file: types_gen.go\n",
			bad:     "output:\n  types:\n    file: 1\n",
			at:      "/output/types/file",
			keyword: "type",
		},
		{
			name:    "output.types.package given a boolean",
			good:    "output:\n  types:\n    package: types\n",
			bad:     "output:\n  types:\n    package: true\n",
			at:      "/output/types/package",
			keyword: "type",
		},
		{
			name:    "output.client.name given an integer",
			good:    "output:\n  client:\n    name: DB\n",
			bad:     "output:\n  client:\n    name: 1\n",
			at:      "/output/client/name",
			keyword: "type",
		},
		{
			name:    "output.client.file given a boolean",
			good:    "output:\n  client:\n    file: db_gen.go\n",
			bad:     "output:\n  client:\n    file: true\n",
			at:      "/output/client/file",
			keyword: "type",
		},
		{
			name:    "generation.batch_size given a string",
			good:    "generation:\n  batch_size: 50\n",
			bad:     "generation:\n  batch_size: \"50\"\n",
			at:      "/generation/batch_size",
			keyword: "type",
		},
		{
			name:    "generation.batch_size given a fraction",
			good:    "generation:\n  batch_size: 50\n",
			bad:     "generation:\n  batch_size: 50.5\n",
			at:      "/generation/batch_size",
			keyword: "type",
		},
		{
			name:    "generation.page_size given a string",
			good:    "generation:\n  page_size: 25\n",
			bad:     "generation:\n  page_size: \"25\"\n",
			at:      "/generation/page_size",
			keyword: "type",
		},
		{
			name:    "generation.page_size given a fraction",
			good:    "generation:\n  page_size: 25\n",
			bad:     "generation:\n  page_size: 25.5\n",
			at:      "/generation/page_size",
			keyword: "type",
		},
		{
			name:    "generation.cursor_keys given a string",
			good:    "generation:\n  cursor_keys: [created_at, id]\n",
			bad:     "generation:\n  cursor_keys: id\n",
			at:      "/generation/cursor_keys",
			keyword: "type",
		},
		{
			name:    "generation.cursor_keys item given an integer",
			good:    "generation:\n  cursor_keys: [created_at, id]\n",
			bad:     "generation:\n  cursor_keys: [1]\n",
			at:      "/generation/cursor_keys/0",
			keyword: "type",
		},
		{
			name:    "generation.uuid_version given an integer",
			good:    "generation:\n  uuid_version: v7\n",
			bad:     "generation:\n  uuid_version: 7\n",
			at:      "/generation/uuid_version",
			keyword: "type",
		},
		{
			name:    "generation.strict_updates given a string",
			good:    "generation:\n  strict_updates: false\n",
			bad:     "generation:\n  strict_updates: \"false\"\n",
			at:      "/generation/strict_updates",
			keyword: "type",
		},
		{
			name:    "generation.soft_delete_columns given an object",
			good:    "generation:\n  soft_delete_columns:\n    - name: deleted_at\n      type: timestamp\n",
			bad:     "generation:\n  soft_delete_columns:\n    name: deleted_at\n    type: timestamp\n",
			at:      "/generation/soft_delete_columns",
			keyword: "type",
		},
		{
			name:    "generation.soft_delete_columns item given a string",
			good:    "generation:\n  soft_delete_columns:\n    - name: deleted_at\n      type: timestamp\n",
			bad:     "generation:\n  soft_delete_columns: [deleted_at]\n",
			at:      "/generation/soft_delete_columns/0",
			keyword: "type",
		},
		{
			name:    "generation.soft_delete_columns item name given an integer",
			good:    "generation:\n  soft_delete_columns:\n    - name: deleted_at\n      type: timestamp\n",
			bad:     "generation:\n  soft_delete_columns:\n    - name: 1\n      type: timestamp\n",
			at:      "/generation/soft_delete_columns/0/name",
			keyword: "type",
		},
		{
			name:    "generation.soft_delete_columns item missing its name",
			good:    "generation:\n  soft_delete_columns:\n    - name: deleted_at\n      type: timestamp\n",
			bad:     "generation:\n  soft_delete_columns:\n    - type: timestamp\n",
			at:      "/generation/soft_delete_columns/0",
			keyword: "required",
		},
		{
			name:    "generation.soft_delete_columns item missing its type",
			good:    "generation:\n  soft_delete_columns:\n    - name: deleted_at\n      type: timestamp\n",
			bad:     "generation:\n  soft_delete_columns:\n    - name: deleted_at\n",
			at:      "/generation/soft_delete_columns/0",
			keyword: "required",
		},
		{
			name:    "generation.update_columns given a string",
			good:    "generation:\n  update_columns: [touched_at]\n",
			bad:     "generation:\n  update_columns: touched_at\n",
			at:      "/generation/update_columns",
			keyword: "type",
		},
		{
			name:    "generation.update_columns item given an integer",
			good:    "generation:\n  update_columns: [touched_at]\n",
			bad:     "generation:\n  update_columns: [1]\n",
			at:      "/generation/update_columns/0",
			keyword: "type",
		},
		{
			name:    "generation.exclude_columns given a string",
			good:    "generation:\n  exclude_columns: [internal_notes]\n",
			bad:     "generation:\n  exclude_columns: internal_notes\n",
			at:      "/generation/exclude_columns",
			keyword: "type",
		},
		{
			name:    "generation.exclude_columns item given an integer",
			good:    "generation:\n  exclude_columns: [internal_notes]\n",
			bad:     "generation:\n  exclude_columns: [1]\n",
			at:      "/generation/exclude_columns/0",
			keyword: "type",
		},
		{
			name:    "generation.nested_mutations.enabled given a string",
			good:    "generation:\n  nested_mutations:\n    enabled: true\n",
			bad:     "generation:\n  nested_mutations:\n    enabled: \"true\"\n",
			at:      "/generation/nested_mutations/enabled",
			keyword: "type",
		},
		{
			name:    "overrides.use_pointers given a string",
			good:    "overrides:\n  use_pointers: false\n",
			bad:     "overrides:\n  use_pointers: \"false\"\n",
			at:      "/overrides/use_pointers",
			keyword: "type",
		},
		{
			name:    "overrides.types given an array",
			good:    "overrides:\n  types:\n    uuid:\n      type: uuid.UUID\n      import: github.com/google/uuid\n",
			bad:     "overrides:\n  types: [uuid]\n",
			at:      "/overrides/types",
			keyword: "type",
		},
		{
			name:    "overrides.types entry given a string",
			good:    "overrides:\n  types:\n    uuid:\n      type: uuid.UUID\n      import: github.com/google/uuid\n",
			bad:     "overrides:\n  types:\n    uuid: uuid.UUID\n",
			at:      "/overrides/types/uuid",
			keyword: "type",
		},
		{
			name:    "overrides.types entry with an unknown key",
			good:    "overrides:\n  types:\n    uuid:\n      type: uuid.UUID\n      import: github.com/google/uuid\n",
			bad:     "overrides:\n  types:\n    uuid:\n      type: uuid.UUID\n      package: github.com/google/uuid\n",
			at:      "/overrides/types/uuid",
			keyword: "additionalProperties",
		},
		{
			name:    "overrides.types entry type given an integer",
			good:    "overrides:\n  types:\n    bigint:\n      type: int64\n",
			bad:     "overrides:\n  types:\n    bigint:\n      type: 64\n",
			at:      "/overrides/types/bigint/type",
			keyword: "type",
		},
		{
			name:    "overrides.types entry import given an integer",
			good:    "overrides:\n  types:\n    uuid:\n      type: uuid.UUID\n      import: github.com/google/uuid\n",
			bad:     "overrides:\n  types:\n    uuid:\n      type: uuid.UUID\n      import: 1\n",
			at:      "/overrides/types/uuid/import",
			keyword: "type",
		},
		{
			name:    "overrides.types entry zero_value given an integer",
			good:    "overrides:\n  types:\n    bigint:\n      type: int64\n      zero_value: \"0\"\n",
			bad:     "overrides:\n  types:\n    bigint:\n      type: int64\n      zero_value: 0\n",
			at:      "/overrides/types/bigint/zero_value",
			keyword: "type",
		},
		{
			name:    "overrides.types entry nullable given an integer",
			good:    "overrides:\n  types:\n    uuid:\n      type: uuid.UUID\n      import: github.com/google/uuid\n      nullable: uuid.NullUUID\n",
			bad:     "overrides:\n  types:\n    uuid:\n      type: uuid.UUID\n      import: github.com/google/uuid\n      nullable: 1\n",
			at:      "/overrides/types/uuid/nullable",
			keyword: "type",
		},
		{
			name:    "overrides.types entry nullable object with an unknown key",
			good:    "overrides:\n  types:\n    uuid:\n      type: uuid.UUID\n      import: github.com/google/uuid\n      nullable:\n        type: uuid.NullUUID\n",
			bad:     "overrides:\n  types:\n    uuid:\n      type: uuid.UUID\n      import: github.com/google/uuid\n      nullable:\n        type: uuid.NullUUID\n        valid: Valid\n",
			at:      "/overrides/types/uuid/nullable",
			keyword: "additionalProperties",
		},
		{
			name:    "overrides.types nullable.valid_field given a boolean",
			good:    "overrides:\n  types:\n    uuid:\n      type: uuid.UUID\n      import: github.com/google/uuid\n      nullable:\n        type: uuid.NullUUID\n        valid_field: Present\n",
			bad:     "overrides:\n  types:\n    uuid:\n      type: uuid.UUID\n      import: github.com/google/uuid\n      nullable:\n        type: uuid.NullUUID\n        valid_field: true\n",
			at:      "/overrides/types/uuid/nullable/valid_field",
			keyword: "type",
		},
		{
			name:    "tables struct_name given an integer",
			good:    "tables:\n  users:\n    struct_name: Account\n",
			bad:     "tables:\n  users:\n    struct_name: 1\n",
			at:      "/tables/users/struct_name",
			keyword: "type",
		},
		{
			name:    "tables column_map name given an integer",
			good:    "tables:\n  users:\n    column_map:\n      email_addr:\n        name: Email\n",
			bad:     "tables:\n  users:\n    column_map:\n      email_addr:\n        name: 1\n",
			at:      "/tables/users/column_map/email_addr/name",
			keyword: "type",
		},
		{
			name:    "tables column_map access given an integer",
			good:    "tables:\n  users:\n    column_map:\n      ssn:\n        access: hidden\n",
			bad:     "tables:\n  users:\n    column_map:\n      ssn:\n        access: 1\n",
			at:      "/tables/users/column_map/ssn/access",
			keyword: "type",
		},
		{
			name:    "relationship discriminator missing its column",
			good:    tscDiscriminatorYAML("column: entity_type\n          value: asset.attachment\n"),
			bad:     tscDiscriminatorYAML("value: asset.attachment\n"),
			at:      "/tables/assets/relationships/0/discriminator",
			keyword: "required",
		},
		{
			name:    "relationship discriminator missing its value",
			good:    tscDiscriminatorYAML("column: entity_type\n          value: asset.attachment\n"),
			bad:     tscDiscriminatorYAML("column: entity_type\n"),
			at:      "/tables/assets/relationships/0/discriminator",
			keyword: "required",
		},
		{
			name:    "relationship discriminator value given an array",
			good:    tscDiscriminatorYAML("column: entity_type\n          value: asset.attachment\n"),
			bad:     tscDiscriminatorYAML("column: entity_type\n          value: [asset.attachment]\n"),
			at:      "/tables/assets/relationships/0/discriminator/value",
			keyword: "type",
		},
		{
			name:    "relationship discriminator value given an object",
			good:    tscDiscriminatorYAML("column: entity_type\n          value: 3\n"),
			bad:     tscDiscriminatorYAML("column: entity_type\n          value:\n            kind: asset\n"),
			at:      "/tables/assets/relationships/0/discriminator/value",
			keyword: "type",
		},
		{
			name:    "relationship discriminator value given null",
			good:    tscDiscriminatorYAML("column: entity_type\n          value: true\n"),
			bad:     tscDiscriminatorYAML("column: entity_type\n          value: null\n"),
			at:      "/tables/assets/relationships/0/discriminator/value",
			keyword: "type",
		},
		{
			name:    "relationship missing its table",
			good:    "tables:\n  assets:\n    relationships:\n      - name: Documents\n        type: o2m\n        table: documents\n        fk: asset_id\n",
			bad:     "tables:\n  assets:\n    relationships:\n      - name: Documents\n        type: o2m\n        fk: asset_id\n",
			at:      "/tables/assets/relationships/0",
			keyword: "required",
		},
		{
			name:    "m2m relationship missing its table",
			good:    "tables:\n  assets:\n    relationships:\n      - name: Tags\n        type: m2m\n        table: tags\n        junction: asset_tags\n",
			bad:     "tables:\n  assets:\n    relationships:\n      - name: Tags\n        type: m2m\n        junction: asset_tags\n",
			at:      "/tables/assets/relationships/0",
			keyword: "required",
		},
		{
			name:    "tables nested_mutations.relationships given an object",
			good:    "tables:\n  assets:\n    nested_mutations:\n      relationships:\n        - name: Attachments\n",
			bad:     "tables:\n  assets:\n    nested_mutations:\n      relationships:\n        name: Attachments\n",
			at:      "/tables/assets/nested_mutations/relationships",
			keyword: "type",
		},
		{
			name:    "tables nested_mutations.relationships item given a string",
			good:    "tables:\n  assets:\n    nested_mutations:\n      relationships:\n        - name: Attachments\n",
			bad:     "tables:\n  assets:\n    nested_mutations:\n      relationships: [Attachments]\n",
			at:      "/tables/assets/nested_mutations/relationships/0",
			keyword: "type",
		},
		{
			name:    "tables nested_mutations.relationships item missing its name",
			good:    "tables:\n  assets:\n    nested_mutations:\n      relationships:\n        - name: Attachments\n          allow_reparent: true\n",
			bad:     "tables:\n  assets:\n    nested_mutations:\n      relationships:\n        - allow_reparent: true\n",
			at:      "/tables/assets/nested_mutations/relationships/0",
			keyword: "required",
		},
		{
			name:    "tables nested_mutations.relationships item with an unknown key",
			good:    "tables:\n  assets:\n    nested_mutations:\n      relationships:\n        - name: Attachments\n",
			bad:     "tables:\n  assets:\n    nested_mutations:\n      relationships:\n        - name: Attachments\n          reparent: true\n",
			at:      "/tables/assets/nested_mutations/relationships/0",
			keyword: "additionalProperties",
		},
		{
			name:    "tables nested_mutations allow_reparent given a string",
			good:    "tables:\n  assets:\n    nested_mutations:\n      relationships:\n        - name: Attachments\n          allow_reparent: true\n",
			bad:     "tables:\n  assets:\n    nested_mutations:\n      relationships:\n        - name: Attachments\n          allow_reparent: \"true\"\n",
			at:      "/tables/assets/nested_mutations/relationships/0/allow_reparent",
			keyword: "type",
		},
		{
			name:    "extras entry description given an integer",
			good:    "extras:\n  Address:\n    description: A postal address.\n",
			bad:     "extras:\n  Address:\n    description: 1\n",
			at:      "/extras/Address/description",
			keyword: "type",
		},
		{
			name:    "extras entry with an unknown key",
			good:    "extras:\n  Address:\n    description: A postal address.\n",
			bad:     "extras:\n  Address:\n    package: addr\n",
			at:      "/extras/Address",
			keyword: "additionalProperties",
		},
		{
			name:    "extras fields given an array",
			good:    "extras:\n  Address:\n    fields:\n      Street:\n        type: string\n",
			bad:     "extras:\n  Address:\n    fields: [Street]\n",
			at:      "/extras/Address/fields",
			keyword: "type",
		},
		{
			name:    "extras field with an unknown key",
			good:    "extras:\n  Address:\n    fields:\n      Street:\n        type: string\n",
			bad:     "extras:\n  Address:\n    fields:\n      Street:\n        go_type: string\n",
			at:      "/extras/Address/fields/Street",
			keyword: "additionalProperties",
		},
		{
			name:    "extras field description given an integer",
			good:    "extras:\n  Address:\n    fields:\n      Street:\n        description: Street line.\n",
			bad:     "extras:\n  Address:\n    fields:\n      Street:\n        description: 1\n",
			at:      "/extras/Address/fields/Street/description",
			keyword: "type",
		},
		{
			name:    "extras field type given an integer",
			good:    "extras:\n  Address:\n    fields:\n      Zip:\n        type: string\n",
			bad:     "extras:\n  Address:\n    fields:\n      Zip:\n        type: 5\n",
			at:      "/extras/Address/fields/Zip/type",
			keyword: "type",
		},
		{
			name:    "extras field import given an integer",
			good:    "extras:\n  Address:\n    fields:\n      ID:\n        type: uuid.UUID\n        import: github.com/google/uuid\n",
			bad:     "extras:\n  Address:\n    fields:\n      ID:\n        type: uuid.UUID\n        import: 1\n",
			at:      "/extras/Address/fields/ID/import",
			keyword: "type",
		},
		{
			name:    "extras field tags given a string",
			good:    "extras:\n  Address:\n    fields:\n      Street:\n        tags:\n          json: street\n",
			bad:     "extras:\n  Address:\n    fields:\n      Street:\n        tags: street\n",
			at:      "/extras/Address/fields/Street/tags",
			keyword: "type",
		},
		{
			name:    "extras field tag value given an integer",
			good:    "extras:\n  Address:\n    fields:\n      Street:\n        tags:\n          json: street\n",
			bad:     "extras:\n  Address:\n    fields:\n      Street:\n        tags:\n          json: 1\n",
			at:      "/extras/Address/fields/Street/tags/json",
			keyword: "type",
		},
		{
			name:    "enums struct_name given an integer",
			good:    "enums:\n  order_status:\n    struct_name: Status\n",
			bad:     "enums:\n  order_status:\n    struct_name: 1\n",
			at:      "/enums/order_status/struct_name",
			keyword: "type",
		},
		{
			name:    "cache.enabled given a string",
			good:    "cache:\n  enabled: true\n",
			bad:     "cache:\n  enabled: \"true\"\n",
			at:      "/cache/enabled",
			keyword: "type",
		},
		{
			name:    "cache.version given a string",
			good:    "cache:\n  version: 2\n",
			bad:     "cache:\n  version: \"2\"\n",
			at:      "/cache/version",
			keyword: "type",
		},
		{
			name:    "cache.version given a fraction",
			good:    "cache:\n  version: 2\n",
			bad:     "cache:\n  version: 2.5\n",
			at:      "/cache/version",
			keyword: "type",
		},
		{
			name:    "cache.ttl given an integer",
			good:    "cache:\n  ttl: 5m\n",
			bad:     "cache:\n  ttl: 300\n",
			at:      "/cache/ttl",
			keyword: "type",
		},
		{
			name:    "cache.serializer given an integer",
			good:    "cache:\n  serializer: msgpack\n",
			bad:     "cache:\n  serializer: 1\n",
			at:      "/cache/serializer",
			keyword: "type",
		},
		{
			name:    "cache.key_prefix given an integer",
			good:    "cache:\n  key_prefix: app\n",
			bad:     "cache:\n  key_prefix: 1\n",
			at:      "/cache/key_prefix",
			keyword: "type",
		},
		{
			name:    "cache.hydration.enabled given a string",
			good:    "cache:\n  hydration:\n    enabled: true\n",
			bad:     "cache:\n  hydration:\n    enabled: \"true\"\n",
			at:      "/cache/hydration/enabled",
			keyword: "type",
		},
		{
			name:    "cache.hydration.timeout given an integer",
			good:    "cache:\n  hydration:\n    timeout: 2s\n",
			bad:     "cache:\n  hydration:\n    timeout: 2\n",
			at:      "/cache/hydration/timeout",
			keyword: "type",
		},
		{
			name:    "cache.circuit_breaker.enabled given a string",
			good:    "cache:\n  circuit_breaker:\n    enabled: true\n",
			bad:     "cache:\n  circuit_breaker:\n    enabled: \"true\"\n",
			at:      "/cache/circuit_breaker/enabled",
			keyword: "type",
		},
		{
			name:    "cache.circuit_breaker.failure_threshold given a string",
			good:    "cache:\n  circuit_breaker:\n    failure_threshold: 5\n",
			bad:     "cache:\n  circuit_breaker:\n    failure_threshold: \"5\"\n",
			at:      "/cache/circuit_breaker/failure_threshold",
			keyword: "type",
		},
		{
			name:    "cache.circuit_breaker.failure_threshold given a fraction",
			good:    "cache:\n  circuit_breaker:\n    failure_threshold: 5\n",
			bad:     "cache:\n  circuit_breaker:\n    failure_threshold: 5.5\n",
			at:      "/cache/circuit_breaker/failure_threshold",
			keyword: "type",
		},
		{
			name:    "cache.circuit_breaker.probe_interval given an integer",
			good:    "cache:\n  circuit_breaker:\n    probe_interval: 30s\n",
			bad:     "cache:\n  circuit_breaker:\n    probe_interval: 30\n",
			at:      "/cache/circuit_breaker/probe_interval",
			keyword: "type",
		},
		{
			name:    "cache.circuit_breaker.half_open_max_probes given a string",
			good:    "cache:\n  circuit_breaker:\n    half_open_max_probes: 2\n",
			bad:     "cache:\n  circuit_breaker:\n    half_open_max_probes: \"2\"\n",
			at:      "/cache/circuit_breaker/half_open_max_probes",
			keyword: "type",
		},
		{
			name:    "cache.circuit_breaker.half_open_max_probes given a fraction",
			good:    "cache:\n  circuit_breaker:\n    half_open_max_probes: 2\n",
			bad:     "cache:\n  circuit_breaker:\n    half_open_max_probes: 2.5\n",
			at:      "/cache/circuit_breaker/half_open_max_probes",
			keyword: "type",
		},
		{
			name:    "events.enabled given a string",
			good:    "events:\n  enabled: true\n",
			bad:     "events:\n  enabled: \"true\"\n",
			at:      "/events/enabled",
			keyword: "type",
		},
		{
			name:    "tenancy.enabled given a string",
			good:    "tenancy:\n  enabled: true\n",
			bad:     "tenancy:\n  enabled: \"true\"\n",
			at:      "/tenancy/enabled",
			keyword: "type",
		},
		{
			name:    "tenancy.column given an integer",
			good:    "tenancy:\n  column: org_id\n",
			bad:     "tenancy:\n  column: 1\n",
			at:      "/tenancy/column",
			keyword: "type",
		},
		{
			name:    "tenancy.required given a string",
			good:    "tenancy:\n  required: false\n",
			bad:     "tenancy:\n  required: \"false\"\n",
			at:      "/tenancy/required",
			keyword: "type",
		},
		{
			name:    "tenancy.type given a string",
			good:    "tenancy:\n  type:\n    type: uuid.UUID\n    import: github.com/google/uuid\n",
			bad:     "tenancy:\n  type: uuid.UUID\n",
			at:      "/tenancy/type",
			keyword: "type",
		},
		{
			name:    "tenancy.type with an unknown key",
			good:    "tenancy:\n  type:\n    type: uuid.UUID\n    import: github.com/google/uuid\n",
			bad:     "tenancy:\n  type:\n    type: uuid.UUID\n    package: github.com/google/uuid\n",
			at:      "/tenancy/type",
			keyword: "additionalProperties",
		},
		{
			name:    "tenancy.type import given an integer",
			good:    "tenancy:\n  type:\n    type: uuid.UUID\n    import: github.com/google/uuid\n",
			bad:     "tenancy:\n  type:\n    type: uuid.UUID\n    import: 1\n",
			at:      "/tenancy/type/import",
			keyword: "type",
		},
		{
			name:    "tenancy.type zero_value given an integer",
			good:    "tenancy:\n  type:\n    type: int64\n    zero_value: \"0\"\n",
			bad:     "tenancy:\n  type:\n    type: int64\n    zero_value: 0\n",
			at:      "/tenancy/type/zero_value",
			keyword: "type",
		},
		{
			name:    "tenancy.type nullable in its full form",
			good:    "tenancy:\n  type:\n    type: uuid.UUID\n    import: github.com/google/uuid\n    nullable:\n      type: uuid.NullUUID\n      underlying_field: UUID\n",
			bad:     "tenancy:\n  type:\n    type: uuid.UUID\n    import: github.com/google/uuid\n    nullable:\n      type: uuid.NullUUID\n      underlying: UUID\n",
			at:      "/tenancy/type/nullable",
			keyword: "additionalProperties",
		},
		{
			name:    "tables overrides.use_pointers set to false",
			good:    "tables:\n  users:\n    overrides:\n      types:\n        uuid:\n          type: uuid.UUID\n          import: github.com/google/uuid\n",
			bad:     "tables:\n  users:\n    overrides:\n      use_pointers: false\n",
			at:      "/tables/users/overrides/use_pointers",
			keyword: "not",
		},
		{
			name:    "tables overrides.use_pointers set to true",
			good:    "tables:\n  users:\n    overrides:\n      types:\n        bigint:\n          type: int64\n",
			bad:     "tables:\n  users:\n    overrides:\n      use_pointers: true\n      types:\n        bigint:\n          type: int64\n",
			at:      "/tables/users/overrides/use_pointers",
			keyword: "not",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := sch.Validate(yamlToJSONValue(t, []byte(tt.good))); err != nil {
				t.Fatalf("Validate(%q) = %v, want nil", tt.good, err)
			}
			tscWantRejectedAt(t, sch, tt.bad, tt.at, tt.keyword)
		})
	}
}

// TestSchemaV1_closedValueSets pins each §4.14 closed value set: every value
// the PRD lists validates, and a value outside the set is rejected by the
// key's `enum`.
func TestSchemaV1_closedValueSets(t *testing.T) {
	sch := compileSchema(t)

	tests := []struct {
		name  string
		yaml  string // the document; %s is replaced by the value
		at    string
		value string
		valid bool
	}{
		{"output.layout single_file", "output:\n  layout: %s\n", "/output/layout", "single_file", true},
		{"output.layout file_per_table", "output:\n  layout: %s\n", "/output/layout", "file_per_table", true},
		{"output.layout per_table", "output:\n  layout: %s\n", "/output/layout", "per_table", false},
		{"output.layout Single_File", "output:\n  layout: %s\n", "/output/layout", "Single_File", false},

		{"generation.uuid_version v4", "generation:\n  uuid_version: %s\n", "/generation/uuid_version", "v4", true},
		{"generation.uuid_version v7", "generation:\n  uuid_version: %s\n", "/generation/uuid_version", "v7", true},
		{"generation.uuid_version v1", "generation:\n  uuid_version: %s\n", "/generation/uuid_version", "v1", false},
		{"generation.uuid_version V7", "generation:\n  uuid_version: %s\n", "/generation/uuid_version", "V7", false},

		{"soft delete column type timestamp", tscSoftDeleteYAML, "/generation/soft_delete_columns/0/type", "timestamp", true},
		{"soft delete column type bool", tscSoftDeleteYAML, "/generation/soft_delete_columns/0/type", "bool", true},
		{"soft delete column type integer", tscSoftDeleteYAML, "/generation/soft_delete_columns/0/type", "integer", true},
		{"soft delete column type boolean", tscSoftDeleteYAML, "/generation/soft_delete_columns/0/type", "boolean", false},
		{"soft delete column type timestamptz", tscSoftDeleteYAML, "/generation/soft_delete_columns/0/type", "timestamptz", false},

		{"column access public", tscAccessYAML, "/tables/users/column_map/ssn/access", "public", true},
		{"column access read_only", tscAccessYAML, "/tables/users/column_map/ssn/access", "read_only", true},
		{"column access write_only", tscAccessYAML, "/tables/users/column_map/ssn/access", "write_only", true},
		{"column access hidden", tscAccessYAML, "/tables/users/column_map/ssn/access", "hidden", true},
		{"column access internal", tscAccessYAML, "/tables/users/column_map/ssn/access", "internal", true},
		{"column access private", tscAccessYAML, "/tables/users/column_map/ssn/access", "private", false},
		{"column access Hidden", tscAccessYAML, "/tables/users/column_map/ssn/access", "Hidden", false},

		{"cache.serializer json", "cache:\n  serializer: %s\n", "/cache/serializer", "json", true},
		{"cache.serializer msgpack", "cache:\n  serializer: %s\n", "/cache/serializer", "msgpack", true},
		{"cache.serializer custom", "cache:\n  serializer: %s\n", "/cache/serializer", "custom", true},
		{"cache.serializer gob", "cache:\n  serializer: %s\n", "/cache/serializer", "gob", false},
		{"cache.serializer JSON", "cache:\n  serializer: %s\n", "/cache/serializer", "JSON", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := strings.Replace(tt.yaml, "%s", tt.value, 1)
			if tt.valid {
				if err := sch.Validate(yamlToJSONValue(t, []byte(src))); err != nil {
					t.Errorf("Validate(%q) = %v, want nil", src, err)
				}
				return
			}
			tscWantRejectedAt(t, sch, src, tt.at, "enum")
		})
	}
}

const (
	tscSoftDeleteYAML = "generation:\n  soft_delete_columns:\n    - name: deleted_at\n      type: %s\n"
	tscAccessYAML     = "tables:\n  users:\n    column_map:\n      ssn:\n        access: %s\n"
)

// tscDiscriminatorYAML wraps discriminator body lines (indented for the
// `discriminator:` key) in a valid o2m relationship.
func tscDiscriminatorYAML(body string) string {
	return "tables:\n  assets:\n    relationships:\n      - name: Attachments\n        type: o2m\n        table: documents\n        fk: entity_id\n        discriminator:\n          " + body
}

// tscWantRejectedAt validates src and requires a leaf validation error whose
// instance is exactly at (a JSON pointer) and whose failing keyword is
// keyword.
func tscWantRejectedAt(t *testing.T, sch *jsonschema.Schema, src, at, keyword string) {
	t.Helper()

	err := sch.Validate(yamlToJSONValue(t, []byte(src)))
	if err == nil {
		t.Errorf("Validate(%q) = nil, want a %q error at %q", src, keyword, at)
		return
	}
	ve, ok := errors.AsType[*jsonschema.ValidationError](err)
	if !ok {
		t.Fatalf("Validate(%q) = %T (%v), want *jsonschema.ValidationError", src, err, err)
	}

	var leaves []string
	for _, leaf := range tscLeaves(ve) {
		if leaf.InstanceLocation == at && strings.HasSuffix(leaf.KeywordLocation, "/"+keyword) {
			return
		}
		leaves = append(leaves, leaf.InstanceLocation+" "+leaf.KeywordLocation)
	}
	t.Errorf("Validate(%q) failed without a %q error at %q; leaf errors:\n  %s",
		src, keyword, at, strings.Join(leaves, "\n  "))
}

// tscLeaves returns the validation errors that have no further causes.
func tscLeaves(ve *jsonschema.ValidationError) []*jsonschema.ValidationError {
	if len(ve.Causes) == 0 {
		return []*jsonschema.ValidationError{ve}
	}
	var out []*jsonschema.ValidationError
	for _, c := range ve.Causes {
		out = append(out, tscLeaves(c)...)
	}
	return out
}
