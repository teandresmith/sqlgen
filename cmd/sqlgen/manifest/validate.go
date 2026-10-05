package manifest

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// compiledSchemaV1 compiles the embedded canonical schema once, on first use.
var compiledSchemaV1 = sync.OnceValues(func() (*jsonschema.Schema, error) {
	sch, err := jsonschema.CompileString("embedded://manifest/schema/v1.json", string(schemaV1))
	if err != nil {
		return nil, fmt.Errorf("compile embedded manifest schema: %w", err)
	}
	return sch, nil
})

// ValidateAgainstSchema validates raw manifest JSON against the embedded
// canonical JSON Schema (schema/v1.json) offline — no network fetch. It is the
// hot-path validator the MCP server's manifest store runs on every (re)load
// (MCP.md §6.3) and the in-memory backing for the `sqlgen_validate_manifest`
// tool (MCP.md §4.1). This differs from `sqlgen manifest validate`, which
// honors a document's `$schema` URL and only falls back to the embedded schema
// (PRD §30.8).
func ValidateAgainstSchema(raw []byte) error {
	sch, err := compiledSchemaV1()
	if err != nil {
		return err
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}
	if err := sch.Validate(doc); err != nil {
		return fmt.Errorf("manifest schema validation: %w", err)
	}
	return nil
}
