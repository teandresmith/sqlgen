package config_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// compileSchema compiles the embedded sqlgen.yml schema once per test.
func compileSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	sch, err := jsonschema.CompileString("embedded://config/schema/v1.json", string(config.SchemaV1()))
	if err != nil {
		t.Fatalf("CompileString(embedded schema) = %v, want nil", err)
	}
	return sch
}

// yamlToJSONValue decodes YAML into the generic Go value shape the JSON Schema
// validator expects. Round-tripping through JSON normalizes the numeric and map
// types yaml.v3 produces.
func yamlToJSONValue(t *testing.T, src []byte) any {
	t.Helper()
	var doc any
	if err := yaml.Unmarshal(src, &doc); err != nil {
		t.Fatalf("yaml.Unmarshal = %v, want nil", err)
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("json.Marshal(yaml doc) = %v, want nil", err)
	}
	var out any
	if err := json.Unmarshal(encoded, &out); err != nil {
		t.Fatalf("json.Unmarshal = %v, want nil", err)
	}
	return out
}

// TestSchemaV1_acceptsExampleConfigs validates every shipped example config
// against the schema. The examples are the broadest real-world config coverage
// in the repo — they exercise tenancy, cache, events, GraphQL, manifest, views,
// per-table overrides and all three dialects. Because the schema sets
// "additionalProperties": false throughout, a config key the schema forgot
// fails here rather than silently degrading a user's editor into red squiggles
// on a valid file.
func TestSchemaV1_acceptsExampleConfigs(t *testing.T) {
	sch := compileSchema(t)

	matches, err := filepath.Glob(filepath.Join("..", "testdata", "examples", "*", "sqlgen.yml"))
	if err != nil {
		t.Fatalf("Glob(examples) = %v, want nil", err)
	}
	if len(matches) == 0 {
		t.Fatal("Glob(examples) matched 0 configs, want at least 1")
	}

	for _, path := range matches {
		t.Run(filepath.Base(filepath.Dir(path)), func(t *testing.T) {
			src, err := os.ReadFile(path) //nolint:gosec // test-controlled path under testdata
			if err != nil {
				t.Fatalf("ReadFile(%s) = %v, want nil", path, err)
			}
			if err := sch.Validate(yamlToJSONValue(t, src)); err != nil {
				t.Errorf("Validate(%s) = %v, want nil", path, err)
			}
		})
	}
}

// TestSchemaV1_rejectsKnownMistakes pins the cases the schema exists to catch.
// Each is a mistake a user can make in an editor that the Go loader would only
// report at `sqlgen generate` time.
func TestSchemaV1_rejectsKnownMistakes(t *testing.T) {
	sch := compileSchema(t)

	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "dialect postgresql instead of postgres",
			yaml: "input:\n  dialect: postgresql\n",
		},
		{
			// `convert:` was removed — an overridden type must implement
			// Scanner/Valuer, or be one the driver special-cases (PRD §4.7).
			// A config carrying the old block has to fail loudly rather than
			// have it silently ignored, which would generate a scan the driver
			// cannot fill.
			name: "removed convert block on a type override",
			yaml: "overrides:\n  types:\n    citext:\n      type: mypkg.CI\n      import: example.com/mypkg\n      convert:\n        db_type: string\n        parse: mypkg.ParseCI\n",
		},
		{
			name: "unknown top-level key",
			yaml: "inputs:\n  dialect: postgres\n",
		},
		{
			name: "misspelled nested key",
			yaml: "output:\n  packge: models\n", //nolint:misspell // deliberate typo: this is the mistake the schema must reject
		},
		{
			name: "invalid driver",
			yaml: "output:\n  driver: pq\n",
		},
		{
			name: "invalid operations preset",
			yaml: "api:\n  operations: readonly\n",
		},
		{
			// PRD §4.6: the client has no operations switch, so an editor
			// flags the key before sqlgen validate names api.operations.
			name: "client operations key under generation",
			yaml: "generation:\n  operations: read_only\n",
		},
		{
			name: "client operations key under a table",
			yaml: "tables:\n  users:\n    operations:\n      hard_delete: false\n",
		},
		{
			// PRD §26.5.1: a client-only key in an API mask.
			name: "get_many in an api.operations mask",
			yaml: "api:\n  operations:\n    get_many: false\n",
		},
		{
			name: "update_many in a per-table api.operations mask",
			yaml: "tables:\n  users:\n    api:\n      operations:\n        update_many: false\n",
		},
		{
			name: "invalid soft delete column type",
			yaml: "generation:\n  soft_delete_columns:\n    - name: deleted_at\n      type: varchar\n",
		},
		{
			name: "invalid column access role",
			yaml: "tables:\n  users:\n    column_map:\n      ssn:\n        access: secret\n",
		},
		{
			name: "invalid graphql field casing",
			yaml: "api:\n  graphql:\n    field_casing: PascalCase\n",
		},
		{
			name: "query_limit as a string",
			yaml: "generation:\n  query_limit: \"1000\"\n",
		},
		{
			// `generation.errors.package` was removed: it was parsed and
			// defaulted but never read, so setting it changed nothing. The
			// replacement is a client hook (PRD §22.4). Because LoadConfig uses
			// non-strict yaml.Unmarshal, a stale key is silently ignored at
			// load — this schema rejection is the *only* signal a consumer
			// gets, so pin it here or the removal is unenforced.
			name: "removed errors package block",
			yaml: "generation:\n  errors:\n    package: github.com/cockroachdb/errors\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := sch.Validate(yamlToJSONValue(t, []byte(tt.yaml))); err == nil {
				t.Errorf("Validate(%q) = nil, want a validation error", tt.yaml)
			}
		})
	}
}

// TestSchemaV1_acceptsBothOperationsForms pins the union type. An
// `api.operations` mask accepts either a preset string or an explicit key map
// (Operations has a custom UnmarshalYAML), so a reflection-derived schema would
// model only the object arm and reject the documented `operations: read_only`
// spelling.
func TestSchemaV1_acceptsBothOperationsForms(t *testing.T) {
	sch := compileSchema(t)

	tests := []struct {
		name string
		yaml string
	}{
		{"preset string", "api:\n  operations: read_only\n"},
		{"key map", "api:\n  operations:\n    preset: all\n    update_where: false\n"},
		{"per-table key map", "tables:\n  users:\n    api:\n      operations:\n        preset: read_only\n        create: true\n"},
		{"nullable shorthand", "overrides:\n  types:\n    uuid:\n      type: uuid.UUID\n      import: github.com/google/uuid\n      nullable: uuid.NullUUID\n"},
		{"nullable full form", "overrides:\n  types:\n    uuid:\n      type: uuid.UUID\n      import: github.com/google/uuid\n      nullable:\n        type: uuid.NullUUID\n        underlying_field: UUID\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := sch.Validate(yamlToJSONValue(t, []byte(tt.yaml))); err != nil {
				t.Errorf("Validate(%q) = %v, want nil", tt.yaml, err)
			}
		})
	}
}

// TestSchemaV1_acceptsUnexercisedSurface covers the config blocks the shipped
// examples never use. The examples validate ~46% of the schema's property
// names, and both over-strict bugs found in review lived in the untested half:
// a `required` on `typeOverride` that neither LoadConfig nor Validate enforces.
// Each case below is a config the Go loader accepts, so the schema must too —
// a false error here is a valid file painted red in the user's editor.
func TestSchemaV1_acceptsUnexercisedSurface(t *testing.T) {
	sch := compileSchema(t)

	tests := []struct {
		name string
		yaml string
	}{
		{
			// validateTenancyTypeOverride only fires when `type` is set, so a
			// bare `import` is accepted by the loader.
			name: "tenancy type override with import but no type",
			yaml: "tenancy:\n  enabled: true\n  type:\n    import: github.com/google/uuid\n",
		},
		{
			name: "type override with no import (builtin Go type)",
			yaml: "overrides:\n  types:\n    bigint:\n      type: int64\n",
		},
		{
			name: "database source with full connection",
			yaml: "input:\n  source: both\n  connection:\n    host: localhost\n    port: 5432\n    user: app\n    database: app\n    ssl_mode: require\n  introspect:\n    schemas: [public]\n    exclude_schemas: [audit]\n    tables: [users]\n",
		},
		{
			name: "connection by url",
			yaml: "input:\n  source: database\n  connection:\n    url: postgres://localhost:5432/app\n",
		},
		{
			name: "manifest with mcp and breadcrumbs",
			yaml: "generation:\n  manifest:\n    enabled: true\n    formats: [json, markdown]\n    json_layout: per_entity\n    json_per_entity_dir: ./manifest/entities\n    markdown_dir: ./manifest/md\n    embed_in_client: true\n    breadcrumbs:\n      claude_md: true\n      agents_md: true\n      package_doc: false\n    mcp:\n      emit_project_config: true\n      project_configs: [.mcp.json]\n      server_key: sqlgen\n      command_template: \"sqlgen mcp\"\n",
		},
		{
			name: "per-table primary key and relationships",
			yaml: "tables:\n  posts:\n    primary_key:\n      strategy: app\n      uuid_version: v7\n      columns: [id]\n    exclude_relationships: [legacy_author]\n    relationships:\n      - name: Tags\n        type: m2m\n        side: parent\n        table: tags\n        junction: post_tags\n        junction_local_fk: post_id\n        junction_reference_fk: tag_id\n        sort:\n          - column: name\n            direction: asc\n",
		},
		{
			name: "api mask and rest and grpc blocks",
			yaml: "api:\n  enabled: true\n  operations: read_only\n  rest:\n    enabled: true\n    base_path: /api/v1\n    spec:\n      file: ./openapi.json\n  grpc:\n    enabled: false\n    proto_dir: ./proto\n    service_dir: ./grpc\n",
		},
		{
			name: "graphql scalars with external marshaling",
			yaml: "api:\n  graphql:\n    enabled: true\n    max_depth: 10\n    max_complexity: 200\n    field_casing: camel_case\n    gqlgen_config: ./gqlgen.yml\n    scalars:\n      IPAddr:\n        go_type: netip.Addr\n        marshaling: external\n        marshaler_package: example.com/scalars\n",
		},
		{
			name: "cache with hydration and circuit breaker",
			yaml: "cache:\n  enabled: true\n  version: 2\n  ttl: 5m\n  serializer: msgpack\n  key_prefix: app\n  hydration:\n    enabled: true\n    timeout: 2s\n  circuit_breaker:\n    enabled: true\n    failure_threshold: 5\n    probe_interval: 30s\n    half_open_max_probes: 2\n",
		},
		{
			name: "extras with tags",
			yaml: "extras:\n  Address:\n    description: A postal address.\n    fields:\n      Street:\n        type: string\n        tags:\n          json: street\n",
		},
		{
			name: "nested mutations block with every key",
			yaml: "generation:\n  nested_mutations:\n    enabled: true\n    operations: [create, update, upsert]\n    verbs: [create, connect, disconnect, clear]\n    max_depth: 1\n",
		},
		{
			name: "the three nested api.operations keys",
			yaml: "api:\n  operations:\n    preset: all\n    create_with_related: true\n    update_with_related: false\n    upsert_with_related: true\n",
		},
		{
			name: "per-table nested mutation allowlist",
			yaml: "tables:\n  assets:\n    nested_mutations:\n      relationships:\n        - name: Attachments\n        - name: Events\n          allow_reparent: true\n",
		},
		{
			name: "relationship discriminator with a string value",
			yaml: "tables:\n  assets:\n    relationships:\n      - name: Attachments\n        type: o2m\n        table: documents\n        fk: entity_id\n        discriminator:\n          column: entity_type\n          value: asset.attachment\n",
		},
		{
			// `value` is a scalar, not a string (PRD §4.8 / §13.4.1), so the
			// schema must accept the non-textual discriminator columns the Go
			// type allows.
			name: "relationship discriminator with an integer value",
			yaml: "tables:\n  assets:\n    relationships:\n      - name: Attachments\n        type: o2m\n        table: documents\n        fk: entity_id\n        discriminator:\n          column: entity_kind\n          value: 3\n",
		},
		{
			name: "relationship discriminator with a boolean value",
			yaml: "tables:\n  assets:\n    relationships:\n      - name: Archived\n        type: o2m\n        table: documents\n        fk: entity_id\n        discriminator:\n          column: is_archived\n          value: true\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := sch.Validate(yamlToJSONValue(t, []byte(tt.yaml))); err != nil {
				t.Errorf("Validate(%q) = %v, want nil", tt.yaml, err)
			}
		})
	}
}

// TestSchemaV1_acceptsPRDFullExample validates the canonical config in PRD
// §4.12 "Full Example" against the schema.
//
// The PRD is the project's source of truth and §4.12 is the config a reader is
// most likely to copy, so it is also the most damaging place for spec/impl
// drift. It had drifted: it documented `output.pagination` and `output.sort`
// blocks that OutputConfig has never had. The loader ignores unknown keys, so
// nothing caught it until the schema did. This test keeps the canonical example
// loadable.
func TestSchemaV1_acceptsPRDFullExample(t *testing.T) {
	prd, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "PRD.md"))
	if err != nil {
		t.Fatalf("ReadFile(docs/PRD.md) = %v, want nil", err)
	}

	example, err := prdFullExample(string(prd))
	if err != nil {
		t.Fatalf("prdFullExample = %v, want nil", err)
	}

	sch := compileSchema(t)
	if err := sch.Validate(yamlToJSONValue(t, []byte(example))); err != nil {
		t.Errorf("Validate(PRD §4.12 full example) = %v, want nil\n\nthe PRD documents config the loader does not accept; fix §4.12 or add the field to the schema", err)
	}
}

// prdFullExample extracts the first fenced yaml block under the §4.12 heading.
func prdFullExample(prd string) (string, error) {
	const heading = "### 4.12 Full Example"

	_, after, found := strings.Cut(prd, heading)
	if !found {
		return "", fmt.Errorf("heading %q not found in docs/PRD.md", heading)
	}
	_, after, found = strings.Cut(after, "```yaml\n")
	if !found {
		return "", fmt.Errorf("no fenced yaml block under %q", heading)
	}
	block, _, found := strings.Cut(after, "\n```")
	if !found {
		return "", fmt.Errorf("unterminated yaml block under %q", heading)
	}
	return block, nil
}

// TestSchemaV1_noDriftFromConfigStructs walks every yaml-tagged field reachable
// from RootConfig alongside the schema node that should declare it, and reports
// any field the schema is missing at that exact path.
//
// Nothing in the build ties the hand-written schema to the config structs, so a
// field added to Go without a matching schema entry would make editors flag a
// valid config as invalid — the exact failure this schema exists to prevent.
//
// The walk is path-aware rather than name-based: it resolves $ref and the
// oneOf arms of the two union types, descends map values through
// additionalProperties and slices through items, and matches each Go field
// against the properties of its own parent object. A name declared elsewhere in
// the schema does not satisfy an unrelated field, so a new `enabled` or `type`
// under a new block is caught. Verified failing-first by deleting
// `json_per_entity_dir`, `marshaler_package`, and `generation.batch_size` —
// the last of which a name-based check misses, because `batch_size` is also
// declared under `tables.<name>`.
//
// It deliberately checks presence, not shape: pinning the schema's
// expressiveness to what reflection can represent would forfeit the union
// types, closed value sets and descriptions that make the schema worth
// hand-writing.
func TestSchemaV1_noDriftFromConfigStructs(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(config.SchemaV1(), &doc); err != nil {
		t.Fatalf("Unmarshal(SchemaV1) = %v, want nil", err)
	}

	w := &schemaWalker{doc: doc, seen: make(map[string]bool)}
	w.walk(reflect.TypeFor[config.RootConfig](), doc, "")

	if len(w.missing) > 0 {
		t.Errorf("config fields with no schema property at their path:\n  %s\n\nadd them to schema/v1.json, or mark the Go field `yaml:\"-\"`",
			strings.Join(w.missing, "\n  "))
	}
}

// schemaWalker pairs a Go type with the schema node expected to describe it.
type schemaWalker struct {
	doc     map[string]any
	seen    map[string]bool
	missing []string
}

// walk descends t and node together, recording every yaml-tagged field that
// node does not declare.
func (w *schemaWalker) walk(t reflect.Type, node map[string]any, path string) {
	node = w.resolve(node)

	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	switch t.Kind() {
	case reflect.Slice, reflect.Array:
		w.walk(t.Elem(), w.child(node, "items"), path+"[]")
		return
	case reflect.Map:
		w.walk(t.Elem(), w.child(node, "additionalProperties"), path+".<key>")
		return
	case reflect.Struct:
	default:
		return
	}

	// Cycle guard keyed on type + path shape, so the same struct reached at two
	// different schema paths is still checked at both.
	key := t.String() + "@" + path
	if w.seen[key] {
		return
	}
	w.seen[key] = true

	props := w.properties(node)

	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if name == "-" || name == "" {
			continue
		}

		sub, ok := props[name]
		if !ok {
			w.missing = append(w.missing, path+"."+name)
			continue
		}
		w.walk(f.Type, sub, path+"."+name)
	}
}

// resolve follows a $ref into $defs. Non-ref nodes are returned unchanged.
func (w *schemaWalker) resolve(node map[string]any) map[string]any {
	for range 10 {
		ref, ok := node["$ref"].(string)
		if !ok {
			return node
		}
		name, found := strings.CutPrefix(ref, "#/$defs/")
		if !found {
			return node
		}
		defs, _ := w.doc["$defs"].(map[string]any)
		next, _ := defs[name].(map[string]any)
		if next == nil {
			return node
		}
		node = next
	}
	return node
}

// properties returns the property set a node declares, merging the arms of
// oneOf / anyOf / allOf so a union type's object arm is reachable.
func (w *schemaWalker) properties(node map[string]any) map[string]map[string]any {
	out := make(map[string]map[string]any)

	if props, ok := node["properties"].(map[string]any); ok {
		for name, sub := range props {
			if m, ok := sub.(map[string]any); ok {
				out[name] = m
			}
		}
	}

	for _, keyword := range []string{"oneOf", "anyOf", "allOf"} {
		arms, ok := node[keyword].([]any)
		if !ok {
			continue
		}
		for _, arm := range arms {
			m, ok := arm.(map[string]any)
			if !ok {
				continue
			}
			for name, sub := range w.properties(w.resolve(m)) {
				if _, exists := out[name]; !exists {
					out[name] = sub
				}
			}
		}
	}
	return out
}

// child returns the subschema under key, resolved through $ref. A missing key
// yields an empty node, which declares no properties — so a Go field under it
// is reported as missing rather than silently skipped.
func (w *schemaWalker) child(node map[string]any, key string) map[string]any {
	sub, ok := node[key].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return w.resolve(sub)
}
