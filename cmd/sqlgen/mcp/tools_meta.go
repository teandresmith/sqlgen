package mcp

import (
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/teandresmith/sqlgen/manifest"
)

// registerMetaTools installs the package-reference tools: get_conventions,
// get_example (MCP.md §4.1).
func registerMetaTools(srv *mcp.Server, d *toolDeps) {
	addTool(srv, "sqlgen_get_conventions",
		"Return the package-wide conventions block: error sentinels, pagination, CallOptions, comparators, soft-delete, and omittable surfaces.",
		getConventions, d)
	addTool(srv, "sqlgen_get_example",
		"Return the canonical Go read/write example snippets for an entity.",
		getExample, d)
}

// GetConventionsInput is the (argument-free) input for sqlgen_get_conventions.
type GetConventionsInput struct{}

// getConventions returns the manifest's package-wide conventions block (MCP.md
// §4.1). The block is passed through verbatim so it stays consistent with the
// sqlgen://conventions resource.
func getConventions(d *toolDeps, _ GetConventionsInput) (manifest.Conventions, error) {
	return d.store.Manifest().Conventions, nil
}

// GetExampleInput is the input for sqlgen_get_example.
type GetExampleInput struct {
	// Entity is the entity name whose examples are wanted.
	Entity string `json:"entity" jsonschema:"the entity name whose examples are wanted"`
	// Op optionally restricts the snippets to "read" or "write".
	Op string `json:"op,omitempty" jsonschema:"restrict to read or write example snippets"`
	// Compact is accepted for surface parity; see getExample for its effect.
	Compact bool `json:"compact,omitempty" jsonschema:"accepted for parity; example snippets are always retained"`
}

// GetExampleOutput carries the canonical example snippets for an entity.
type GetExampleOutput struct {
	Entity string   `json:"entity"`
	Read   []string `json:"read"`
	Write  []string `json:"write"`
}

// getExample returns an entity's canonical Go snippets (MCP.md §4.1). op filters
// to read or write; the default returns both. An entity with no recorded
// examples yields empty arrays, not an error. compact is accepted for API-shape
// parity with the other per-entity tools, but the snippets are this tool's whole
// payload — its required identity — so they are always retained (MCP.md §4.1's
// "required identity is always retained"). An unknown entity yields -32003 with
// fuzzy suggestions.
func getExample(d *toolDeps, in GetExampleInput) (GetExampleOutput, error) {
	e, ok := d.store.EntityByName(in.Entity)
	if !ok {
		return GetExampleOutput{}, notFoundError(CodeEntityNotFound,
			fmt.Sprintf("entity not found: %s", in.Entity), in.Entity, d.store.EntityNames())
	}
	out := GetExampleOutput{Entity: e.Name, Read: []string{}, Write: []string{}}
	op := strings.TrimSpace(in.Op)
	if e.Examples != nil {
		if op == "" || op == "read" {
			out.Read = append(out.Read, e.Examples.Read...)
		}
		if op == "" || op == "write" {
			out.Write = append(out.Write, e.Examples.Write...)
		}
	}
	return out, nil
}
