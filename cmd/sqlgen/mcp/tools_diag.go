package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/santhosh-tekuri/jsonschema/v5"

	buildmanifest "github.com/teandresmith/sqlgen/cmd/sqlgen/manifest"
)

// registerDiagTools installs the diagnostic tools: health, validate_manifest
// (MCP.md §4.1).
func registerDiagTools(srv *mcp.Server, d *toolDeps) {
	addTool(srv, "sqlgen_health",
		"Report whether the server is serving fresh manifest data: paths, versions, load timestamps, watch state, and ok flag.",
		health, d)
	addTool(srv, "sqlgen_validate_manifest",
		"Validate the loaded manifest (or a manifest at the given path) against the embedded JSON Schema.",
		validateManifest, d)
}

// HealthInput is the (argument-free) input for sqlgen_health.
type HealthInput struct{}

// HealthOutput is the sqlgen_health diagnostic payload. It carries no manifest
// data — only server/manifest state (MCP.md §4.1). Timestamps are RFC 3339;
// last_reload_at is omitted until the first watch-triggered reload.
type HealthOutput struct {
	ManifestPath  string `json:"manifest_path"`
	SchemaVersion string `json:"schema_version"`
	GeneratedAt   string `json:"generated_at"`
	LoadedAt      string `json:"loaded_at"`
	LastReloadAt  string `json:"last_reload_at,omitempty"`
	WatchEnabled  bool   `json:"watch_enabled"`
	SqlgenVersion string `json:"sqlgen_version"`
	OK            bool   `json:"ok"`
}

// health snapshots the store's diagnostic state (MCP.md §4.1). The loaded_at /
// last_reload_at timestamps are set once per (re)load, so two calls within one
// process return byte-identical results (MCP.md §6.5 determinism holds within a
// run).
func health(d *toolDeps, _ HealthInput) (HealthOutput, error) {
	h := d.store.Health()
	out := HealthOutput{
		ManifestPath:  h.ManifestPath,
		SchemaVersion: h.SchemaVersion,
		GeneratedAt:   h.GeneratedAt,
		LoadedAt:      formatTime(h.LoadedAt),
		WatchEnabled:  d.watchEnabled,
		SqlgenVersion: d.version,
		OK:            h.OK,
	}
	if !h.LastReloadAt.IsZero() {
		out.LastReloadAt = formatTime(h.LastReloadAt)
	}
	return out, nil
}

// formatTime renders t in RFC 3339 (UTC), or "" for the zero time.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// ValidateManifestInput is the input for sqlgen_validate_manifest.
type ValidateManifestInput struct {
	// Path optionally names a manifest file to validate instead of the loaded
	// one. When set, the file is read and checked without swapping the server's
	// loaded manifest.
	Path string `json:"path,omitempty" jsonschema:"validate the manifest at this path instead of the loaded one"`
}

// ValidationIssue is one schema-validation failure, matching the §30.8 CLI
// tool's field/message shape.
type ValidationIssue struct {
	Field string `json:"field"`
	Msg   string `json:"msg"`
}

// ValidateManifestOutput is the sqlgen_validate_manifest result.
type ValidateManifestOutput struct {
	Valid         bool              `json:"valid"`
	SchemaVersion string            `json:"schema_version"`
	Errors        []ValidationIssue `json:"errors"`
}

// validateManifest validates the loaded manifest in-memory (no path) or a file
// at the given path against the embedded JSON Schema (MCP.md §4.1). The path
// mode never swaps the server's loaded manifest. A path that cannot be read is
// an internal error; a manifest that parses but fails validation returns
// valid=false with the flattened schema errors.
func validateManifest(d *toolDeps, in ValidateManifestInput) (ValidateManifestOutput, error) {
	raw := d.store.Raw()
	if in.Path != "" {
		data, err := os.ReadFile(in.Path) //nolint:gosec // the tool validates a user-supplied manifest path by design.
		if err != nil {
			return ValidateManifestOutput{}, newToolError(CodeInternal, fmt.Sprintf("read manifest %s: %v", in.Path, err))
		}
		raw = data
	}

	out := ValidateManifestOutput{Errors: []ValidationIssue{}, SchemaVersion: schemaVersionOf(raw)}
	if err := buildmanifest.ValidateAgainstSchema(raw); err != nil {
		out.Valid = false
		out.Errors = flattenSchemaErrors(err)
		return out, nil
	}
	out.Valid = true
	return out, nil
}

// schemaVersionOf best-effort extracts the schema_version from raw manifest
// bytes; it returns "" when the bytes do not parse.
func schemaVersionOf(raw []byte) string {
	var probe struct {
		SchemaVersion string `json:"schema_version"`
	}
	_ = json.Unmarshal(raw, &probe)
	return probe.SchemaVersion
}

// flattenSchemaErrors turns a validation error into the flat field/message list
// the tool reports. A *jsonschema.ValidationError tree is flattened to its leaf
// causes (instance location -> field); any other error becomes a single issue.
func flattenSchemaErrors(err error) []ValidationIssue {
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return []ValidationIssue{{Field: "", Msg: err.Error()}}
	}
	var issues []ValidationIssue
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			field := e.InstanceLocation
			if field == "" {
				field = "/"
			}
			issues = append(issues, ValidationIssue{Field: field, Msg: e.Message})
			return
		}
		for _, c := range e.Causes {
			walk(c)
		}
	}
	walk(ve)
	if len(issues) == 0 {
		issues = []ValidationIssue{{Field: "", Msg: err.Error()}}
	}
	return issues
}
