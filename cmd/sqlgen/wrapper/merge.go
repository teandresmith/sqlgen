package wrapper

import (
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"
)

// MergeInput holds the sqlgen-owned entries that the wrapper merges into the
// consumer's gqlgen.yml at invocation time. Consumer-authored keys always
// win on collision: if the consumer already declared a `models:` entry for
// a given GraphQL type or scalar, sqlgen never overwrites it. See PRD §26.5.6.
type MergeInput struct {
	// SchemaGlob is appended to the gqlgen `schema:` list when missing
	// (e.g. "graph/*.graphqls"). Empty string skips schema merging.
	SchemaGlob string
	// Models maps GraphQL type or scalar names to the Go model paths sqlgen
	// would bind. Each value is a list because gqlgen accepts both single-
	// path strings and multi-path lists; single-element lists are emitted
	// in scalar form for readability.
	Models map[string][]string
	// ModelFields carries the per-field overrides sqlgen dictates for the
	// types gqlgen generates, keyed by GraphQL type then GraphQL field. Each
	// entry becomes `models.<Type>.fields.<field>.{fieldName,type}`, both of
	// which gqlgen's modelgen honours in place of its own derivation — the
	// name in place of `templates.ToGo`, the type in place of the scalar's
	// `Model[0]` binding.
	//
	// `fieldName` is what lets sqlgen's translators reference
	// gqlgen-generated identifiers by name without predicting how gqlgen
	// would have spelled them (PRD §26.5.6 "Go field naming").
	// `type` is what lets a numeric-array input field carry the model's own
	// element width instead of gqlgen's default `[]int` / `[]float64`, which
	// no Go conversion bridges.
	//
	// A `fields:` entry does not bind the type — gqlgen treats a type as
	// consumer-supplied only when it carries a `model:` path — so listing a
	// type here leaves it generated, as intended.
	ModelFields map[string]map[string]ModelField
	// ForceSkipValidation overrides the consumer's `skip_validation:` value
	// in the temp config (consumer-owned file on disk is unchanged). Set
	// under single-file layout: gqlgen's post-generate validation
	// runs `go build` against a Resolver struct whose Client/Q/M fields it
	// just wiped — validation fails before sqlgen's post-subprocess merge
	// can restore them. Forcing the flag lets the subprocess return cleanly
	// so mergeSingleFileResolver can re-add the fields; the consumer's
	// downstream `go build` (or `make`) catches any actual breakage. Under
	// follow-schema this is unset — gqlgen's validation runs and remains
	// useful because the resolver.go scaffold's Client/Q/M survive
	// gqlgen's pass intact.
	ForceSkipValidation bool
}

// ModelField is one `models.<Type>.fields.<field>` entry. Both keys are
// optional and independent: an empty value emits no key, so a field that only
// needs a Go name emits only `fieldName`.
type ModelField struct {
	// FieldName becomes `fieldName:` — the Go identifier gqlgen gives the
	// field.
	FieldName string
	// GoType becomes `type:` — the Go type gqlgen gives the field on a type
	// it generates, overriding its own scalar binding.
	GoType string
}

// Merge merges sqlgen-owned entries into the consumer's gqlgen YAML.
//
// The consumer's structure (key order, comments where preserved by yaml.v3,
// existing entries) is left intact; sqlgen entries are appended only when the
// corresponding key is absent from the consumer's mapping. The result is
// returned as YAML bytes suitable for writing to a temp file.
func Merge(consumerYAML []byte, in MergeInput) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(consumerYAML, &root); err != nil {
		return nil, fmt.Errorf("parsing gqlgen.yml: %w", err)
	}

	doc := ensureDocumentMapping(&root)
	if doc == nil {
		return nil, fmt.Errorf("gqlgen.yml top level must be a YAML mapping")
	}

	if in.SchemaGlob != "" {
		if err := mergeSchemaGlob(doc, in.SchemaGlob); err != nil {
			return nil, err
		}
	}

	if len(in.Models) > 0 || len(in.ModelFields) > 0 {
		if err := mergeModels(doc, in.Models, in.ModelFields); err != nil {
			return nil, err
		}
	}

	if in.ForceSkipValidation {
		setSkipValidation(doc, true)
	}

	out, err := yaml.Marshal(&root)
	if err != nil {
		return nil, fmt.Errorf("marshaling merged gqlgen.yml: %w", err)
	}
	return out, nil
}

// ensureDocumentMapping returns the top-level mapping node from the parsed
// document, materialising an empty mapping when the document is empty. Returns
// nil if the document content is present but not a mapping (a hard error).
func ensureDocumentMapping(root *yaml.Node) *yaml.Node {
	if root.Kind == 0 {
		root.Kind = yaml.DocumentNode
	}
	if len(root.Content) == 0 {
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.MappingNode})
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return nil
	}
	return doc
}

// findChild returns the value node for the given key in a mapping node, or
// nil if the key is absent.
func findChild(mapping *yaml.Node, key string) *yaml.Node {
	if mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

// mergeSchemaGlob ensures the schema glob appears in the schema sequence.
// When the schema key is absent it is added as a one-element list; when it
// already holds the glob the call is a no-op; when it holds a different
// scalar value it is promoted to a list with both entries.
func mergeSchemaGlob(doc *yaml.Node, glob string) error {
	schemaNode := findChild(doc, "schema")
	if schemaNode == nil {
		keyNode := scalarNode("schema")
		seqNode := &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{scalarNode(glob)}}
		doc.Content = append(doc.Content, keyNode, seqNode)
		return nil
	}

	switch schemaNode.Kind {
	case yaml.SequenceNode:
		for _, n := range schemaNode.Content {
			if n.Value == glob {
				return nil
			}
		}
		schemaNode.Content = append(schemaNode.Content, scalarNode(glob))
	case yaml.ScalarNode:
		if schemaNode.Value == glob {
			return nil
		}
		existing := schemaNode.Value
		schemaNode.Kind = yaml.SequenceNode
		schemaNode.Tag = ""
		schemaNode.Value = ""
		schemaNode.Style = 0
		schemaNode.Content = []*yaml.Node{scalarNode(existing), scalarNode(glob)}
	default:
		return fmt.Errorf("schema: must be a string or list (got node kind %d)", schemaNode.Kind)
	}
	return nil
}

// mergeModels merges sqlgen-owned model entries into the models: mapping.
//
// Two independent kinds of entry land here. A `model:` path binds a GraphQL
// type to a Go type the consumer already has; those follow the consumer-wins
// rule — if the type is present at all, sqlgen leaves it alone, because the
// consumer has taken ownership of the binding. A `fields:` entry only renames
// the Go field on a type gqlgen generates; those merge per field, so sqlgen
// can name the fields of a type the consumer has also configured for some
// other reason (a directive, a forced resolver) without discarding their work.
//
// Per-field precedence still favours the consumer: an explicit fieldName in
// their file is never overwritten.
func mergeModels(doc *yaml.Node, models map[string][]string, modelFields map[string]map[string]ModelField) error {
	modelsNode := findChild(doc, "models")
	if modelsNode == nil {
		keyNode := scalarNode("models")
		modelsNode = &yaml.Node{Kind: yaml.MappingNode}
		doc.Content = append(doc.Content, keyNode, modelsNode)
	}
	if modelsNode.Kind != yaml.MappingNode {
		return fmt.Errorf("models: must be a mapping (got node kind %d)", modelsNode.Kind)
	}

	keys := make([]string, 0, len(models))
	for k := range models {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		paths := models[k]
		if len(paths) == 0 {
			continue
		}
		if findChild(modelsNode, k) != nil {
			continue
		}
		modelsNode.Content = append(modelsNode.Content, scalarNode(k), buildModelEntry(paths))
	}

	return mergeModelFields(modelsNode, modelFields)
}

// mergeModelFields writes the per-field overrides under each type's `fields:`
// mapping, creating the type entry when absent. A key the consumer already
// wrote is left untouched, per key rather than per field — a consumer who
// named a field keeps their `fieldName` and still receives sqlgen's `type`.
func mergeModelFields(modelsNode *yaml.Node, modelFields map[string]map[string]ModelField) error {
	types := make([]string, 0, len(modelFields))
	for t := range modelFields {
		types = append(types, t)
	}
	sort.Strings(types)

	for _, typeName := range types {
		fields := modelFields[typeName]
		if len(fields) == 0 {
			continue
		}

		typeNode := findChild(modelsNode, typeName)
		if typeNode == nil {
			typeNode = &yaml.Node{Kind: yaml.MappingNode}
			modelsNode.Content = append(modelsNode.Content, scalarNode(typeName), typeNode)
		}
		if typeNode.Kind != yaml.MappingNode {
			return fmt.Errorf("models.%s: must be a mapping (got node kind %d)", typeName, typeNode.Kind)
		}

		fieldsNode := findChild(typeNode, "fields")
		if fieldsNode == nil {
			fieldsNode = &yaml.Node{Kind: yaml.MappingNode}
			typeNode.Content = append(typeNode.Content, scalarNode("fields"), fieldsNode)
		}
		if fieldsNode.Kind != yaml.MappingNode {
			return fmt.Errorf("models.%s.fields: must be a mapping (got node kind %d)", typeName, fieldsNode.Kind)
		}

		names := make([]string, 0, len(fields))
		for f := range fields {
			names = append(names, f)
		}
		sort.Strings(names)

		for _, graphQLName := range names {
			entry := findChild(fieldsNode, graphQLName)
			if entry == nil {
				entry = &yaml.Node{Kind: yaml.MappingNode}
				fieldsNode.Content = append(fieldsNode.Content, scalarNode(graphQLName), entry)
			}
			if entry.Kind != yaml.MappingNode {
				return fmt.Errorf("models.%s.fields.%s: must be a mapping (got node kind %d)", typeName, graphQLName, entry.Kind)
			}
			writeModelFieldKeys(entry, fields[graphQLName])
		}
	}
	return nil
}

// writeModelFieldKeys writes the sqlgen-owned keys onto one
// `models.<Type>.fields.<field>` entry. Precedence is per KEY, not per field
// (§26.5.6): a consumer who wrote `fieldName` keeps theirs and still receives
// sqlgen's `type`, and vice versa. An empty value writes no key at all —
// `type: ""` would bind the field to the empty Go type.
func writeModelFieldKeys(entry *yaml.Node, f ModelField) {
	if f.FieldName != "" && findChild(entry, "fieldName") == nil {
		entry.Content = append(entry.Content, scalarNode("fieldName"), scalarNode(f.FieldName))
	}
	if f.GoType != "" && findChild(entry, "type") == nil {
		entry.Content = append(entry.Content, scalarNode("type"), scalarNode(f.GoType))
	}
}

// buildModelEntry builds a `model: ...` mapping value for one type or scalar.
// Single-element lists are emitted as a scalar string; multi-element lists
// keep the sequence form (matching the gqlgen documentation conventions).
func buildModelEntry(paths []string) *yaml.Node {
	keyNode := scalarNode("model")
	var inner *yaml.Node
	if len(paths) == 1 {
		inner = scalarNode(paths[0])
	} else {
		seq := &yaml.Node{Kind: yaml.SequenceNode}
		for _, p := range paths {
			seq.Content = append(seq.Content, scalarNode(p))
		}
		inner = seq
	}
	return &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{keyNode, inner}}
}

// scalarNode constructs a string scalar yaml.Node.
func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

// boolScalarNode constructs a boolean scalar yaml.Node.
func boolScalarNode(v bool) *yaml.Node {
	val := "false"
	if v {
		val = "true"
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: val}
}

// setSkipValidation forces the top-level `skip_validation` key to v. When the
// key is absent it is appended; when present its existing scalar is rewritten
// in place so the YAML's key ordering is preserved. The on-disk consumer file
// is unaffected — this only mutates the in-memory doc that gets written to
// the temp config gqlgen sees.
func setSkipValidation(doc *yaml.Node, v bool) {
	if existing := findChild(doc, "skip_validation"); existing != nil {
		existing.Kind = yaml.ScalarNode
		existing.Tag = "!!bool"
		existing.Value = "true"
		if !v {
			existing.Value = "false"
		}
		existing.Style = 0
		existing.Content = nil
		return
	}
	doc.Content = append(doc.Content, scalarNode("skip_validation"), boolScalarNode(v))
}
