package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/spf13/cobra"

	buildmanifest "github.com/teandresmith/sqlgen/cmd/sqlgen/manifest"
	"github.com/teandresmith/sqlgen/manifest"
)

// schemaFetchFunc fetches the JSON Schema referenced by a manifest's $schema
// URL. It is a package variable so tests can stub network access; production
// wiring uses httpFetchSchema.
type schemaFetchFunc func(ctx context.Context, rawURL string) ([]byte, error)

// manifestSchemaFetcher resolves a $schema URL to the schema bytes. Overridden
// in tests to keep them hermetic.
var manifestSchemaFetcher schemaFetchFunc = httpFetchSchema

// maxSchemaBytes caps a fetched schema document to guard against a hostile or
// runaway $schema URL.
const maxSchemaBytes = 4 << 20 // 4 MiB

// newManifestValidateCmd builds `sqlgen manifest validate <path>`. It loads the
// manifest JSON, validates it against the schema referenced by its $schema URL
// (falling back to the embedded schema when the URL is unreachable), and prints
// a one-line summary. Exit codes: 0 success, 1 schema failure, 2 I/O failure
// (PRD §30.8).
func newManifestValidateCmd(flags *cliFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "validate <path>",
		Short: "Validate a manifest JSON file against its JSON Schema",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runManifestValidate(cmd, flags, args[0])
		},
	}
}

func runManifestValidate(cmd *cobra.Command, flags *cliFlags, path string) error {
	errOut := cmd.ErrOrStderr()

	data, err := os.ReadFile(path) //nolint:gosec // CLI validates a user-supplied manifest path by design.
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "Error: read manifest: %v\n", err)
		return &exitError{code: manifestExitIOFail, err: err}
	}

	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		_, _ = fmt.Fprintf(errOut, "Error: parse manifest %s: %v\n", path, err)
		return &exitError{code: manifestExitFail, err: err}
	}

	schema, err := resolveSchema(cmd.Context(), data, flags, errOut)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "Error: %v\n", err)
		return &exitError{code: manifestExitFail, err: err}
	}

	if err := schema.Validate(doc); err != nil {
		_, _ = fmt.Fprintf(errOut, "Error: manifest %s failed schema validation:\n%v\n", path, err)
		return &exitError{code: manifestExitFail, err: fmt.Errorf("manifest schema validation failed")}
	}

	if !flags.quiet {
		printManifestSummary(cmd.OutOrStdout(), data)
	}
	return nil
}

// resolveSchema compiles the JSON Schema for validation. It reads the $schema
// URL from the manifest and fetches it; on any failure — offline, unreachable,
// no URL, or a fetched body that does not compile — it falls back to the
// embedded (known-good) schema (PRD §30.8).
func resolveSchema(ctx context.Context, manifestData []byte, flags *cliFlags, errOut io.Writer) (*jsonschema.Schema, error) {
	var probe struct {
		Schema string `json:"$schema"`
	}
	// A missing/garbled $schema is not fatal — fall back to the embedded schema.
	_ = json.Unmarshal(manifestData, &probe)

	if probe.Schema != "" {
		if sch, err := fetchAndCompileSchema(ctx, probe.Schema); err == nil {
			return sch, nil
		} else if flags.verbose {
			_, _ = fmt.Fprintf(errOut, "note: %v; using embedded schema\n", err)
		}
	}

	sch, err := jsonschema.CompileString("embedded", string(buildmanifest.SchemaV1()))
	if err != nil {
		return nil, fmt.Errorf("compile embedded manifest schema: %w", err)
	}
	return sch, nil
}

// fetchAndCompileSchema fetches the schema at rawURL and compiles it. Either
// step failing is reported so the caller degrades to the embedded schema.
func fetchAndCompileSchema(ctx context.Context, rawURL string) (*jsonschema.Schema, error) {
	body, err := manifestSchemaFetcher(ctx, rawURL)
	if err != nil {
		return nil, fmt.Errorf("could not fetch %s (%w)", rawURL, err)
	}
	sch, err := jsonschema.CompileString(rawURL, string(body))
	if err != nil {
		return nil, fmt.Errorf("fetched schema %s did not compile (%w)", rawURL, err)
	}
	return sch, nil
}

// printManifestSummary prints the schema version and entity / enum / extra
// counts. It decodes into the runtime Document, whose Entities is the
// lightweight index under both layouts, so the counts are layout-agnostic.
func printManifestSummary(out io.Writer, data []byte) {
	var d manifest.Document
	if err := json.Unmarshal(data, &d); err != nil {
		return
	}
	_, _ = fmt.Fprintf(out, "manifest valid: schema_version=%s entities=%d enums=%d extras=%d\n",
		d.SchemaVersion, len(d.Entities), len(d.Enums), len(d.Extras))
}

// httpFetchSchema fetches a schema document over HTTP(S) with a bounded timeout
// and response size. Only http/https URLs are honored; anything else is treated
// as unreachable so the caller falls back to the embedded schema.
func httpFetchSchema(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse $schema url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported $schema scheme %q", u.Scheme)
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build schema request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch schema: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch schema: unexpected status %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSchemaBytes))
	if err != nil {
		return nil, fmt.Errorf("read schema body: %w", err)
	}
	return body, nil
}
