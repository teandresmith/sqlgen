package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	buildmanifest "github.com/teandresmith/sqlgen/cmd/sqlgen/manifest"
)

// Resource URIs (MCP.md §4.2). All bodies are application/json.
const (
	resourceManifestURI    = "sqlgen://manifest"
	resourceConventionsURI = "sqlgen://conventions"
	resourceConfigURI      = "sqlgen://config"
	// resourceEntityTemplate is an RFC 6570 URI template; a concrete read of
	// sqlgen://entity/User routes to entityResource via the SDK's template match.
	resourceEntityTemplate = "sqlgen://entity/{name}"
	resourceEntityPrefix   = "sqlgen://entity/"
)

// mimeJSON is the media type advertised for every resource.
const mimeJSON = "application/json"

// manifestWarnThreshold is the encoded-manifest size (bytes) above which
// sqlgen://manifest attaches a warning advising the consumer to prefer targeted
// tool calls (MCP.md §4.2). It is a heuristic, not an enforced cap — the full
// document is always served — and is revisitable if real-world manifests trend
// larger or smaller.
const manifestWarnThreshold = 256 * 1024

// metaWarningKey is the resource-contents _meta key carrying a size / staleness
// warning. The SDK's ResourceContents is a fixed struct with no sibling
// "warning" field, so the warning of the §4.2 example rides in the protocol's
// sanctioned _meta channel rather than as a literal top-level key. Flagged for
// /verify as an SDK-shape adaptation of §4.2 (parallels the 19.3 IsError
// adaptation of §5.3).
const metaWarningKey = "warning"

// registerResources installs the four §4.2 resources: three fixed URIs
// (sqlgen://manifest, sqlgen://conventions, sqlgen://config) plus the
// sqlgen://entity/{name} template. Like tools, it is called at startup and again
// on every manifest reload; re-registering fires notifications/resources/list_changed
// to connected stdio clients (MCP.md §5.2). The handlers read through the store,
// so they always serve the current good manifest without needing re-registration
// to refresh their data — the re-registration exists solely to emit the change
// notification.
func (s *Server) registerResources() {
	d := s.toolDeps()
	s.mcp.AddResource(
		&mcp.Resource{Name: "manifest", URI: resourceManifestURI, MIMEType: mimeJSON, Description: "The full manifest_gen.json document. Prefer targeted tools (sqlgen_list_entities, sqlgen_get_entity) for large manifests."},
		func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return manifestResource(d, req.Params.URI)
		},
	)
	s.mcp.AddResource(
		&mcp.Resource{Name: "conventions", URI: resourceConventionsURI, MIMEType: mimeJSON, Description: "The package-wide conventions block: error sentinels, pagination, CallOptions, comparators, soft-delete, and omittable surfaces."},
		func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return conventionsResource(d, req.Params.URI)
		},
	)
	s.mcp.AddResource(
		&mcp.Resource{Name: "config", URI: resourceConfigURI, MIMEType: mimeJSON, Description: "The resolved generation-config block: which features (cache, events, soft-delete, tenancy, views, …) the generated package was built with."},
		func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return configResource(d, req.Params.URI)
		},
	)
	s.mcp.AddResourceTemplate(
		&mcp.ResourceTemplate{Name: "entity", URITemplate: resourceEntityTemplate, MIMEType: mimeJSON, Description: "Per-entity manifest record addressed by name, e.g. sqlgen://entity/User."},
		func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return entityResource(d, req.Params.URI)
		},
	)
}

// jsonResource builds a single-content JSON resource result for uri, optionally
// attaching a _meta warning. text is the resource body (already-encoded JSON).
func jsonResource(uri, text, warning string) *mcp.ReadResourceResult {
	c := &mcp.ResourceContents{URI: uri, MIMEType: mimeJSON, Text: text}
	if warning != "" {
		c.Meta = mcp.Meta{metaWarningKey: warning}
	}
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{c}}
}

// manifestResource serves the full manifest_gen.json bytes verbatim (MCP.md
// §4.2). When the encoded body exceeds manifestWarnThreshold it attaches a
// warning steering the consumer to targeted tool calls; the full document is
// served regardless.
func manifestResource(d *toolDeps, uri string) (*mcp.ReadResourceResult, error) {
	raw := d.store.Raw()
	warning := ""
	if len(raw) > manifestWarnThreshold {
		warning = fmt.Sprintf("manifest is %d KB (above %d KB threshold) — prefer sqlgen_list_entities + sqlgen_get_entity over reading the full resource",
			len(raw)/1024, manifestWarnThreshold/1024)
	}
	return jsonResource(uri, string(raw), warning), nil
}

// conventionsResource serves the package-wide conventions block (MCP.md §4.2),
// kept consistent with the sqlgen_get_conventions tool.
func conventionsResource(d *toolDeps, uri string) (*mcp.ReadResourceResult, error) {
	body, err := json.MarshalIndent(d.store.Manifest().Conventions, "", "  ")
	if err != nil {
		return nil, &jsonrpc.Error{Code: int64(CodeInternal), Message: fmt.Sprintf("encode conventions: %v", err)}
	}
	return jsonResource(uri, string(body), ""), nil
}

// configResource serves the resolved generation-config block (MCP.md §4.2). A
// manifest that predates the generation_config block returns an
// empty object plus a warning rather than a misleading all-false config, which a
// zero-valued struct would otherwise produce.
func configResource(d *toolDeps, uri string) (*mcp.ReadResourceResult, error) {
	cfg, present := d.store.GenerationConfig()
	if !present {
		return jsonResource(uri, "{}",
			fmt.Sprintf("generation_config field absent — manifest predates the generation_config addendum (schema %s)", buildmanifest.SchemaVersion)), nil
	}
	body, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, &jsonrpc.Error{Code: int64(CodeInternal), Message: fmt.Sprintf("encode config: %v", err)}
	}
	return jsonResource(uri, string(body), ""), nil
}

// entityResource serves one entity's full manifest record addressed by
// sqlgen://entity/<name> (MCP.md §4.2). An unknown name yields a -32003
// ENTITY_NOT_FOUND JSON-RPC error carrying fuzzy suggestions (MCP.md §5.3).
//
// Resource reads have no IsError channel — the tool-layer escape hatch used
// to keep the code + suggestions visible past the go-sdk client — so the
// error is a genuine protocol-level *jsonrpc.Error. The wire response carries
// code -32003 and data.suggestions correctly (verified by the direct-handler
// test and the raw-JSON-RPC integration leg); the go-sdk *client* library
// collapses that into an opaque error keeping only the message. To keep the
// suggestions useful to an agent driven by that client, they are also inlined
// into the message text — the same belt-and-suspenders the tool envelope uses.
func entityResource(d *toolDeps, uri string) (*mcp.ReadResourceResult, error) {
	name := entityNameFromURI(uri)
	e, ok := d.store.EntityByName(name)
	if !ok {
		return nil, entityNotFoundRPCError(name, d.store.EntityNames())
	}
	body, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return nil, &jsonrpc.Error{Code: int64(CodeInternal), Message: fmt.Sprintf("encode entity %s: %v", name, err)}
	}
	return jsonResource(uri, string(body), ""), nil
}

// entityNameFromURI extracts the <name> segment from a sqlgen://entity/<name>
// URI. Entity names are Go identifiers, so no URL unescaping is required; a URI
// without the prefix (never routed here by the template match) yields "".
func entityNameFromURI(uri string) string {
	return strings.TrimPrefix(uri, resourceEntityPrefix)
}

// entityNotFoundRPCError builds the -32003 protocol error for an unknown entity
// resource, embedding fuzzy suggestions in the JSON-RPC data field so an agent
// can self-correct the URI (MCP.md §5.3).
func entityNotFoundRPCError(name string, candidates []string) error {
	suggestions := suggest(name, candidates)
	msg := fmt.Sprintf("entity not found: %s", name)
	if len(suggestions) > 0 {
		msg += " (did you mean: " + strings.Join(suggestions, ", ") + "?)"
	}
	data, err := json.Marshal(struct {
		Suggestions []string `json:"suggestions"`
	}{Suggestions: suggestions})
	if err != nil {
		// suggestions is a plain []string; marshaling cannot realistically fail.
		return &jsonrpc.Error{Code: int64(CodeEntityNotFound), Message: msg}
	}
	return &jsonrpc.Error{Code: int64(CodeEntityNotFound), Message: msg, Data: data}
}
