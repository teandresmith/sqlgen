package manifest

import _ "embed"

// schemaV1 is the canonical manifest JSON Schema (draft 2020-12), embedded so
// the CLI can validate a manifest offline when the document's $schema URL is
// unreachable (PRD §30.8).
//
//go:embed schema/v1.json
var schemaV1 []byte

// SchemaV1 returns the embedded canonical JSON Schema for the manifest (PRD
// §30.4). It is the offline fallback for `sqlgen manifest validate` when the
// document's $schema URL cannot be fetched. The returned bytes are read-only.
func SchemaV1() []byte { return schemaV1 }
