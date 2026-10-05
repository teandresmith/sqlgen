package wrapper

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestMerge_LayeredOnExistingMergesSqlgenEntries(t *testing.T) {
	consumer := []byte(`schema:
  - "graph/custom.graphqls"
exec:
  filename: graph/generated_gen.go
  package: graph
directives:
  hasRole:
    skip_runtime: true
models:
  HandWrittenAuditLog:
    model: example.com/myapp/internal/audit.Log
`)

	merged, err := Merge(consumer, MergeInput{
		SchemaGlob: "graph/*.graphqls",
		Models: map[string][]string{
			"Product": {"example.com/myapp/internal/models.Product"},
			"UUID":    {"github.com/google/uuid.UUID"},
		},
	})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	got := unmarshalMap(t, merged)

	// Consumer's custom directive must survive.
	directives, ok := got["directives"].(map[string]any)
	if !ok {
		t.Fatalf("directives missing or wrong shape: %T", got["directives"])
	}
	if _, ok := directives["hasRole"]; !ok {
		t.Errorf("directive hasRole was dropped during merge")
	}

	// Schema glob must include both consumer entry AND sqlgen entry.
	schema, ok := got["schema"].([]any)
	if !ok {
		t.Fatalf("schema is not a list: %T", got["schema"])
	}
	if !sliceContainsString(schema, "graph/custom.graphqls") {
		t.Errorf("consumer schema entry was dropped")
	}
	if !sliceContainsString(schema, "graph/*.graphqls") {
		t.Errorf("sqlgen schema glob was not merged in")
	}

	// Models must contain consumer hand-bound model AND sqlgen-managed entries.
	models, ok := got["models"].(map[string]any)
	if !ok {
		t.Fatalf("models is not a mapping: %T", got["models"])
	}
	for _, key := range []string{"HandWrittenAuditLog", "Product", "UUID"} {
		if _, ok := models[key]; !ok {
			t.Errorf("models entry %q is missing from merged output", key)
		}
	}

	// Consumer's exec block must still be present.
	if _, ok := got["exec"]; !ok {
		t.Errorf("consumer exec block was dropped")
	}
}

func TestMerge_ConsumerWinsOnScalarCollision(t *testing.T) {
	consumer := []byte(`models:
  UUID:
    model: example.com/myapp/types.UUID
`)
	merged, err := Merge(consumer, MergeInput{
		Models: map[string][]string{
			"UUID": {"github.com/google/uuid.UUID"},
		},
	})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	got := unmarshalMap(t, merged)
	models := got["models"].(map[string]any)
	uuid := models["UUID"].(map[string]any)
	if uuid["model"] != "example.com/myapp/types.UUID" {
		t.Errorf("consumer-authored UUID binding was overwritten: got %v, want example.com/myapp/types.UUID", uuid["model"])
	}
}

func TestMerge_ConsumerWinsOnModelCollision(t *testing.T) {
	consumer := []byte(`models:
  Product:
    model: example.com/myapp/handwritten.Product
`)
	merged, err := Merge(consumer, MergeInput{
		Models: map[string][]string{
			"Product": {"example.com/myapp/internal/models.Product"},
		},
	})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	got := unmarshalMap(t, merged)
	models := got["models"].(map[string]any)
	product := models["Product"].(map[string]any)
	if product["model"] != "example.com/myapp/handwritten.Product" {
		t.Errorf("consumer-authored Product binding was overwritten: got %v", product["model"])
	}
}

// TestMerge_ModelFieldsWritesGoNames pins the field-naming mechanism:
// sqlgen states the Go field name for every identifier its generated code
// references on a gqlgen-generated type, instead of predicting what gqlgen's
// capitalizer would produce. A missing or mis-shaped entry here surfaces as an
// `undefined field` compile error inside generated code, so the YAML shape is
// worth pinning directly.
func TestMerge_ModelFieldsWritesGoNames(t *testing.T) {
	merged, err := Merge([]byte("schema:\n  - \"graph/custom.graphqls\"\n"), MergeInput{
		ModelFields: map[string]map[string]ModelField{
			"EventFilter": {"line2ID": {FieldName: "Line2ID"}, "mac": {FieldName: "MAC"}},
			"Query":       {"csvRecord": {FieldName: "CSVRecord"}},
		},
	})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	got := unmarshalMap(t, merged)
	models, ok := got["models"].(map[string]any)
	if !ok {
		t.Fatalf("models missing or wrong shape: %T", got["models"])
	}

	for _, tc := range []struct{ typeName, field, want string }{
		{"EventFilter", "line2ID", "Line2ID"},
		{"EventFilter", "mac", "MAC"},
		{"Query", "csvRecord", "CSVRecord"},
	} {
		if got := fieldNameAt(t, models, tc.typeName, tc.field); got != tc.want {
			t.Errorf("models.%s.fields.%s.fieldName = %q, want %q", tc.typeName, tc.field, got, tc.want)
		}
	}

	// A fields-only entry must not carry a model: path — that is what keeps
	// gqlgen generating the type instead of binding it to a consumer struct.
	entry, _ := models["EventFilter"].(map[string]any)
	if _, hasModel := entry["model"]; hasModel {
		t.Error("fields-only entry emitted a model: path, which would make gqlgen bind the type instead of generating it")
	}
}

// TestMerge_ModelFieldsConsumerFieldWins pins per-field precedence. Unlike a
// model: binding — where a consumer entry takes the whole type — a fields:
// entry merges field by field, so sqlgen can name the fields of a type the
// consumer configured for an unrelated reason without discarding their work.
func TestMerge_ModelFieldsConsumerFieldWins(t *testing.T) {
	consumer := []byte(`models:
  EventFilter:
    fields:
      mac:
        fieldName: MacAddress
`)

	merged, err := Merge(consumer, MergeInput{
		ModelFields: map[string]map[string]ModelField{
			"EventFilter": {"mac": {FieldName: "MAC"}, "line2ID": {FieldName: "Line2ID"}},
		},
	})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	models := unmarshalMap(t, merged)["models"].(map[string]any)
	if got := fieldNameAt(t, models, "EventFilter", "mac"); got != "MacAddress" {
		t.Errorf("consumer fieldName for mac = %q, want it preserved as %q", got, "MacAddress")
	}
	if got := fieldNameAt(t, models, "EventFilter", "line2ID"); got != "Line2ID" {
		t.Errorf("sqlgen fieldName for line2ID = %q, want %q", got, "Line2ID")
	}
}

// TestMerge_ModelFieldsCoexistWithModelBinding pins that field overrides land
// on a type sqlgen also binds via model: — the two entries are independent, and
// dropping either would break a different consumer of the merged config.
func TestMerge_ModelFieldsCoexistWithModelBinding(t *testing.T) {
	merged, err := Merge(nil, MergeInput{
		Models:      map[string][]string{"Event": {"example.com/app/models.Event"}},
		ModelFields: map[string]map[string]ModelField{"Event": {"line2ID": {FieldName: "Line2ID"}}},
	})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	models := unmarshalMap(t, merged)["models"].(map[string]any)
	entry, ok := models["Event"].(map[string]any)
	if !ok {
		t.Fatalf("models.Event missing or wrong shape: %T", models["Event"])
	}
	if entry["model"] != "example.com/app/models.Event" {
		t.Errorf("model path = %v, want it preserved alongside fields", entry["model"])
	}
	if got := fieldNameAt(t, models, "Event", "line2ID"); got != "Line2ID" {
		t.Errorf("fieldName = %q, want %q", got, "Line2ID")
	}
}

// TestMerge_ModelFieldsAbsentWhenUnset pins that an empty map leaves models:
// untouched, so a consumer without the GraphQL API sees no sqlgen entries.
func TestMerge_ModelFieldsAbsentWhenUnset(t *testing.T) {
	merged, err := Merge([]byte("schema:\n  - \"graph/custom.graphqls\"\n"), MergeInput{})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if _, ok := unmarshalMap(t, merged)["models"]; ok {
		t.Error("models emitted for an empty MergeInput, want the key absent")
	}
}

// fieldNameAt reads models.<typeName>.fields.<field>.fieldName out of an
// unmarshalled config, returning "" when any level is missing.
func fieldNameAt(t *testing.T, models map[string]any, typeName, field string) string {
	t.Helper()
	return modelKeyAt(t, models, typeName, field, "fieldName")
}

// modelKeyAt reads one key off a `models.<type>.fields.<field>` entry.
func modelKeyAt(t *testing.T, models map[string]any, typeName, field, key string) string {
	t.Helper()
	entry, ok := models[typeName].(map[string]any)
	if !ok {
		return ""
	}
	fields, ok := entry["fields"].(map[string]any)
	if !ok {
		return ""
	}
	f, ok := fields[field].(map[string]any)
	if !ok {
		return ""
	}
	v, _ := f[key].(string)
	return v
}

// TestMerge_ModelFieldsWritesDictatedGoType pins the write-side half of the
// numeric-width fix at the YAML boundary: a numeric-array input field carries `type:`
// alongside `fieldName:`, which is what makes gqlgen emit the model's own
// `[]int32` instead of its default `[]int`. Both keys land on ONE entry —
// splitting them across two would be two nodes gqlgen reads independently.
func TestMerge_ModelFieldsWritesDictatedGoType(t *testing.T) {
	merged, err := Merge(nil, MergeInput{
		ModelFields: map[string]map[string]ModelField{
			"CreateArrInput": {
				"scores": {FieldName: "Scores", GoType: "[]int32"},
				"tags":   {FieldName: "Tags"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	models := unmarshalMap(t, merged)["models"].(map[string]any)
	if got := modelKeyAt(t, models, "CreateArrInput", "scores", "type"); got != "[]int32" {
		t.Errorf("scores type = %q, want %q", got, "[]int32")
	}
	if got := modelKeyAt(t, models, "CreateArrInput", "scores", "fieldName"); got != "Scores" {
		t.Errorf("scores fieldName = %q, want %q — type must not displace the name", got, "Scores")
	}
	// A field with no dictated type must emit no `type:` key at all; an empty
	// one would bind the field to the empty Go type.
	if got := modelKeyAt(t, models, "CreateArrInput", "tags", "type"); got != "" {
		t.Errorf("tags type = %q, want no type key", got)
	}
}

// TestMerge_ModelFieldsConsumerTypeWins pins that precedence is per KEY, not
// per field: a consumer who dictated the Go type keeps it, and still receives
// the fieldName they did not state.
func TestMerge_ModelFieldsConsumerTypeWins(t *testing.T) {
	consumer := []byte(`models:
  CreateArrInput:
    fields:
      scores:
        type: "[]int64"
`)
	merged, err := Merge(consumer, MergeInput{
		ModelFields: map[string]map[string]ModelField{
			"CreateArrInput": {"scores": {FieldName: "Scores", GoType: "[]int32"}},
		},
	})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	models := unmarshalMap(t, merged)["models"].(map[string]any)
	if got := modelKeyAt(t, models, "CreateArrInput", "scores", "type"); got != "[]int64" {
		t.Errorf("consumer type = %q, want it preserved as %q", got, "[]int64")
	}
	if got := modelKeyAt(t, models, "CreateArrInput", "scores", "fieldName"); got != "Scores" {
		t.Errorf("sqlgen fieldName = %q, want %q", got, "Scores")
	}
}

func TestMerge_MalformedYAMLErrorPath(t *testing.T) {
	// Tab characters at indent positions are illegal in YAML mappings.
	bad := []byte("schema:\n\t- broken\n")
	_, err := Merge(bad, MergeInput{SchemaGlob: "graph/*.graphqls"})
	if err == nil {
		t.Fatalf("expected parse error from malformed yaml, got nil")
	}
	if !strings.Contains(err.Error(), "parsing gqlgen.yml") {
		t.Errorf("error %q is missing the sqlgen-flavoured prefix", err)
	}
}

func TestMerge_EmptyConsumerYAMLProducesValidConfig(t *testing.T) {
	merged, err := Merge(nil, MergeInput{
		SchemaGlob: "graph/*.graphqls",
		Models:     map[string][]string{"UUID": {"github.com/google/uuid.UUID"}},
	})
	if err != nil {
		t.Fatalf("Merge on empty input: %v", err)
	}
	got := unmarshalMap(t, merged)
	schema := got["schema"].([]any)
	if !sliceContainsString(schema, "graph/*.graphqls") {
		t.Errorf("schema glob missing from empty-input merge")
	}
	models := got["models"].(map[string]any)
	if _, ok := models["UUID"]; !ok {
		t.Errorf("UUID model missing from empty-input merge")
	}
}

func TestMerge_DoesNotDuplicateSchemaGlob(t *testing.T) {
	consumer := []byte(`schema:
  - "graph/*.graphqls"
`)
	merged, err := Merge(consumer, MergeInput{SchemaGlob: "graph/*.graphqls"})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	got := unmarshalMap(t, merged)
	schema := got["schema"].([]any)
	count := 0
	for _, s := range schema {
		if s == "graph/*.graphqls" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("schema glob duplicated: count=%d, want 1", count)
	}
}

// TestMerge_AliasBindingPathPreserved is the wrapper-side regression guard
// for envelope binding. The envelope binding points the `<T>Connection` / `<T>ListResult`
// schema types at per-table generic type aliases the codegen emits into the
// consumer's models package — `<pkg>.<T>Connection` (resolving to
// `Connection[<T>]` via Go 1.24+ generic type aliases). The YAML encoder
// must round-trip the alias path verbatim so gqlgen's `code.Unalias()` lookup
// reaches the underlying instantiated struct unchanged.
func TestMerge_AliasBindingPathPreserved(t *testing.T) {
	const wantPath = "example.com/foo/gen.ProductConnection"
	merged, err := Merge(nil, MergeInput{
		Models: map[string][]string{
			"ProductConnection": {wantPath},
		},
	})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	got := unmarshalMap(t, merged)
	models, ok := got["models"].(map[string]any)
	if !ok {
		t.Fatalf("models is not a mapping: %T", got["models"])
	}
	entry, ok := models["ProductConnection"].(map[string]any)
	if !ok {
		t.Fatalf("ProductConnection entry is not a mapping: %T", models["ProductConnection"])
	}
	if entry["model"] != wantPath {
		t.Errorf("ProductConnection.model = %q, want %q", entry["model"], wantPath)
	}
}

func TestMerge_PromotesScalarSchemaToList(t *testing.T) {
	consumer := []byte(`schema: graph/custom.graphqls
`)
	merged, err := Merge(consumer, MergeInput{SchemaGlob: "graph/*.graphqls"})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	got := unmarshalMap(t, merged)
	schema, ok := got["schema"].([]any)
	if !ok {
		t.Fatalf("schema must be promoted to list, got %T", got["schema"])
	}
	if !sliceContainsString(schema, "graph/custom.graphqls") || !sliceContainsString(schema, "graph/*.graphqls") {
		t.Errorf("promoted list missing entries: %v", schema)
	}
}

func unmarshalMap(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal merged yaml: %v\n---\n%s", err, b)
	}
	return m
}

func sliceContainsString(values []any, want string) bool {
	for _, v := range values {
		if s, ok := v.(string); ok && s == want {
			return true
		}
	}
	return false
}
