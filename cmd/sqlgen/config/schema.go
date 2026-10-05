package config

import _ "embed"

// schemaV1 is the canonical sqlgen.yml JSON Schema (draft 2020-12), embedded so
// `sqlgen init` can emit a modeline pointing at it and tooling can validate a
// config offline.
//
//go:embed schema/v1.json
var schemaV1 []byte

// SchemaV1 returns the embedded canonical JSON Schema for sqlgen.yml. The
// schema covers structure, key names and closed value sets only — cross-field
// rules (driver/dialect pairing, required connection, soft-delete column type
// compatibility, cursor-key existence) live in Validate and are not expressible
// in JSON Schema. The returned bytes are read-only.
func SchemaV1() []byte { return schemaV1 }

// SchemaV1URL is the published location of the schema returned by [SchemaV1].
// `sqlgen init` writes it into the generated config as a yaml-language-server
// modeline, which is what gives editors completion and hover documentation
// without any per-user setup or schema-registry entry.
const SchemaV1URL = "https://raw.githubusercontent.com/teandresmith/sqlgen/main/cmd/sqlgen/config/schema/v1.json"

// SchemaModeline is the comment `sqlgen init` prepends to a generated
// sqlgen.yml so yaml-language-server (VS Code, JetBrains, Neovim) resolves the
// schema. It carries no trailing newline.
const SchemaModeline = "# yaml-language-server: $schema=" + SchemaV1URL
