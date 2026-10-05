package cli

import (
	"fmt"
	"io"
	"os"
	"slices"
	"time"

	"github.com/spf13/cobra"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/manifest"
	"github.com/teandresmith/sqlgen/parser"
)

// makeGenerateRunE returns the RunE for the generate command, closed over the
// shared flag values so no package-level state is needed.
func makeGenerateRunE(flags *cliFlags) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		start := time.Now()
		out := cmd.OutOrStdout()
		errOut := cmd.ErrOrStderr()

		// 1. Load config.
		cfg, err := config.LoadConfig(flags.config)
		if err != nil {
			_, _ = fmt.Fprintf(errOut, "Error: %v\n", err)
			if flags.config == "" {
				_, _ = fmt.Fprintln(errOut, "Hint: run 'sqlgen init' to create a config file")
			}
			return &exitError{code: ExitConfig, err: err}
		}

		// 2. Validate phase 1 (pre-parse).
		warnings, err := config.ValidatePreParse(cfg)
		if err != nil {
			_, _ = fmt.Fprintf(errOut, "Config validation error: %v\n", err)
			return &exitError{code: ExitConfig, err: err}
		}
		printWarnings(errOut, warnings, flags.quiet)

		// 3. Parse schema.
		schema, err := parseSchema(cfg)
		if err != nil {
			code := ExitSchema
			if cfg.Input.Source == config.SourceDatabase || cfg.Input.Source == config.SourceBoth {
				code = ExitConnection
			}
			printSchemaError(errOut, err)
			return &exitError{code: code, err: err}
		}
		// Statements the parser skipped, e.g. CREATE VIEW in DDL (PRD §16).
		printManifestWarnings(errOut, schema.Warnings, flags.quiet)

		// 4. Validate phase 2 (post-parse). Runs against the raw schema,
		// before PK overrides are resolved — see applyPrimaryKeyOverrides.
		schemaTables := toSchemaTables(schema)
		schemaViews := toSchemaViews(schema)
		postWarnings, err := config.ValidatePostParse(cfg, schemaTables, schemaViews)
		if err != nil {
			_, _ = fmt.Fprintf(errOut, "Post-parse validation error: %v\n", err)
			return &exitError{code: ExitConfig, err: err}
		}
		printWarnings(errOut, postWarnings, flags.quiet)

		// 5. Resolve PK overrides, then detect relationships.
		applyPrimaryKeyOverrides(schema, cfg)
		parser.DetectRelationships(schema)

		// 6. Generate.
		result, err := gen.Generate(schema, cfg, Version)
		if err != nil {
			_, _ = fmt.Fprintf(errOut, "Generation error: %v\n", err)
			return &exitError{code: ExitGeneration, err: err}
		}
		printManifestWarnings(errOut, result.Warnings, flags.quiet)

		// 7. Stale file cleanup (file_per_table only).
		var staleGenFiles []string
		if cfg.Output.Layout == config.LayoutFilePerTable {
			staleGenFiles, err = gen.CleanStaleFiles(cfg.Output.Dir, result.Files)
			if err != nil {
				_, _ = fmt.Fprintf(errOut, "Cleanup error: %v\n", err)
				return &exitError{code: ExitGeneration, err: err}
			}
		}

		// 7b. Chained GraphQL generation. When api.graphql.enabled
		// is true and the consumer hasn't opted out via --no-graphql or
		// SQLGEN_NO_GRAPHQL, run the gqlgen wrapper inline so the consumer's
		// `go generate` step produces a compiling package. The escape hatch
		// is for consumers running gqlgen in a separate CI step or migrating
		// incrementally.
		graphqlFiles, err := chainGraphQLGenIfEnabled(cmd, cfg, flags, schema, out, errOut)
		if err != nil {
			return err
		}

		// 7c. Manifest stage (PRD §30.3). Runs after all _gen.go emission,
		// stale cleanup, and the chained GraphQL step so it reflects the final
		// generated surface and its own artifact (manifest_embed_gen.go)
		// survives gen's stale sweep. It lives here rather than inside
		// gen.Generate because cmd/sqlgen/manifest imports gen (an import cycle
		// would form). When the manifest is disabled it only prunes prior
		// artifacts. Warnings name user-owned breadcrumbs sqlgen left untouched.
		manifestResult, err := manifest.RunStage(manifest.StageInput{
			Schema:    schema,
			Config:    cfg,
			Tables:    result.Tables,
			Views:     result.Views,
			Enums:     result.Enums,
			API:       result.API,
			Version:   Version,
			Timestamp: resolveManifestTimestamp(flags.manifestTimestamp),
		})
		if err != nil {
			// manifestResult still carries whatever the sweep removed before it
			// failed — name it before bailing, so a partial cleanup is never
			// the thing the consumer has to discover on their own.
			if manifestResult != nil {
				printDeleted(errOut, manifestResult.Deleted, flags.quiet)
			}
			_, _ = fmt.Fprintf(errOut, "Manifest error: %v\n", err)
			return &exitError{code: ExitGeneration, err: err}
		}
		printManifestWarnings(errOut, manifestResult.Warnings, flags.quiet)
		// Manifest artifacts are §23.8's governed exception to the
		// `_gen.go`-only cleanup rule, so their removals belong in the same
		// reported set — a swept manifest directory was previously silent.
		deletedFiles := slices.Concat(staleGenFiles, manifestResult.Deleted)

		// 7d. Project-local MCP config emission (MCP.md §3.4/§6.6). Runs as
		// the last manifest-stage step, after breadcrumbs, so the manifest
		// path and sqlgen version are final at write time. It lives here
		// rather than inside manifest.RunStage because the merge logic is in
		// cmd/sqlgen/mcp, which imports cmd/sqlgen/manifest (an import cycle
		// would form) — the CLI coordinates.
		mcpWarnings, err := emitMCPProjectConfigs(cfg, Version)
		printManifestWarnings(errOut, mcpWarnings, flags.quiet)
		if err != nil {
			_, _ = fmt.Fprintf(errOut, "MCP config error: %v\n", err)
			return &exitError{code: ExitGeneration, err: err}
		}

		// 8. Report.
		printGenerateReport(out, flags, schema, result, deletedFiles, graphqlFiles, time.Since(start))
		return nil
	}
}

// printDeleted names artifacts a failed manifest sweep already removed. The
// successful path folds the same paths into the generate report instead
// (printGenerateReport), which only lists them under --verbose; on the error
// path they are printed unconditionally, because the run is about to abort and
// the report never happens.
func printDeleted(errOut io.Writer, deleted []string, quiet bool) {
	if quiet {
		return
	}
	for _, f := range deleted {
		_, _ = fmt.Fprintf(errOut, "deleted: %s\n", f)
	}
}

// resolveManifestTimestamp returns the --manifest-timestamp flag value verbatim
// when set (pinning generated_at for byte-deterministic manifest output, PRD
// §30.3 Determinism), otherwise the current UTC time in RFC3339. The value only
// reaches the manifest when manifest.enabled is true; it is inert otherwise.
func resolveManifestTimestamp(flag string) string {
	if flag != "" {
		return flag
	}
	return time.Now().UTC().Format(time.RFC3339)
}

// printManifestWarnings surfaces the user-owned-file warnings returned by the
// manifest stage (breadcrumbs sqlgen declined to overwrite, PRD §30.3), mirroring
// printWarnings' format. Suppressed in quiet mode.
func printManifestWarnings(w io.Writer, warnings []string, quiet bool) {
	if quiet {
		return
	}
	for _, warn := range warnings {
		_, _ = fmt.Fprintf(w, "Warning: %s\n", warn)
	}
}

// printGenerateReport emits the per-run summary for `sqlgen generate`. The
// chained GraphQL invocation contributes seed-file paths via graphqlFiles —
// they fold into the verbose listing and the headline file
// count so the reported totals stay accurate when gqlgen runs inline.
func printGenerateReport(out io.Writer, flags *cliFlags, schema *parser.Schema, result *gen.GenerateResult, deletedFiles, graphqlFiles []string, elapsed time.Duration) {
	if flags.verbose {
		printVerbose(out, schema, result, elapsed)
		for _, f := range graphqlFiles {
			_, _ = fmt.Fprintf(out, "  graphql seed: %s\n", f)
		}
		for _, f := range deletedFiles {
			_, _ = fmt.Fprintf(out, "  deleted: %s\n", f)
		}
		return
	}
	if flags.quiet {
		return
	}
	totalFiles := len(result.Files) + len(graphqlFiles)
	_, _ = fmt.Fprintf(out, "sqlgen: generated %d files (%d tables, %d views) in %s\n",
		totalFiles, result.TableCount, result.ViewCount, elapsed.Round(time.Millisecond))
}

// chainGraphQLGenIfEnabled invokes the gqlgen wrapper inline when the
// chaining gate fires. Returns the seed files written by the
// wrapper so the caller can fold them into the generate report; returns nil
// + nil error when the gate decides to skip.
func chainGraphQLGenIfEnabled(cmd *cobra.Command, cfg *config.RootConfig, flags *cliFlags, schema *parser.Schema, out, errOut io.Writer) ([]string, error) {
	if !shouldChainGraphQLGen(cfg, flags) {
		return nil, nil
	}
	res, err := runGraphQLGen(cmd.Context(), cfg, schema, runGraphQLGenOptions{
		Stdout: out,
		Stderr: errOut,
	})
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	return res.SeedFiles, nil
}

// shouldChainGraphQLGen is the chaining gate: returns true when the
// `sqlgen generate` run should follow up with `sqlgen graphql gen`. Both the
// --no-graphql flag and the SQLGEN_NO_GRAPHQL env var (any non-empty value)
// suppress the chain so consumers running gqlgen in a separate CI step or
// migrating incrementally can keep the historical two-step behaviour.
func shouldChainGraphQLGen(cfg *config.RootConfig, flags *cliFlags) bool {
	if flags.noGraphQL {
		return false
	}
	if os.Getenv("SQLGEN_NO_GRAPHQL") != "" {
		return false
	}
	if cfg.API == nil || !cfg.API.Enabled {
		return false
	}
	if cfg.API.GraphQL == nil || !cfg.API.GraphQL.Enabled {
		return false
	}
	return true
}
