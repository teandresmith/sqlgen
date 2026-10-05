package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// makeValidateRunE returns the RunE for the validate command. It runs the same
// pipeline as generate (config → validate → parse → validate) but stops before
// code generation and collects all errors to report together.
func makeValidateRunE(flags *cliFlags) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
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

		var allErrors []string
		var allWarnings []config.Warning
		var allGenWarnings []string
		exitCode := ExitSuccess

		// 2. Pre-parse validation.
		warnings, err := config.ValidatePreParse(cfg)
		allWarnings = append(allWarnings, warnings...)
		if err != nil {
			exitCode = ExitConfig
			allErrors = append(allErrors, splitJoinedErrors(err)...)
		}

		// 3. Parse schema — attempt even if pre-parse had errors to collect
		// as much feedback as possible.
		schema, err := parseSchema(cfg)
		if err != nil {
			if exitCode == ExitSuccess {
				exitCode = ExitSchema
			}
			allErrors = append(allErrors, splitJoinedErrors(err)...)
		}

		// 4. Post-parse validation, whenever there is a schema to check. An
		// unresolved PostgreSQL foreign key comes back with the schema (see
		// parseSchema), so the config errors below are reported in the same
		// run rather than after the FK is fixed. Runs against the
		// raw schema, before PK overrides are resolved — see
		// applyPrimaryKeyOverrides.
		if schema != nil {
			// Statements the parser skipped, e.g. CREATE VIEW in DDL (PRD §16).
			allGenWarnings = append(allGenWarnings, schema.Warnings...)
			schemaTables := toSchemaTables(schema)
			schemaViews := toSchemaViews(schema)
			postWarnings, err := config.ValidatePostParse(cfg, schemaTables, schemaViews)
			allWarnings = append(allWarnings, postWarnings...)
			if err != nil {
				if exitCode == ExitSuccess {
					exitCode = ExitConfig
				}
				allErrors = append(allErrors, splitJoinedErrors(err)...)
			}

			// Resolve PK overrides, then detect relationships — the order
			// ValidateGeneration below depends on.
			applyPrimaryKeyOverrides(schema, cfg)
			parser.DetectRelationships(schema)

			// 4b. Generation-phase validation. `validate` used to stop at the
			// two config passes above, so a config that only `generate`
			// rejects — a resolved-field-name collision, a per-column type
			// literal with no importable package, an ambiguous soft-delete
			// column — printed "config and schema are valid" and then failed
			// on the next generate. ValidateGeneration builds the
			// contexts in memory and discards them: no templates are loaded
			// and no files are written.
			genWarnings, err := gen.ValidateGeneration(schema, cfg)
			allGenWarnings = append(allGenWarnings, genWarnings...)
			if err != nil {
				if exitCode == ExitSuccess {
					exitCode = ExitConfig
				}
				allErrors = append(allErrors, splitJoinedErrors(err)...)
			}
		}

		// 5. Report.
		printWarnings(errOut, allWarnings, flags.quiet)
		printManifestWarnings(errOut, allGenWarnings, flags.quiet)

		if len(allErrors) > 0 {
			for _, e := range allErrors {
				_, _ = fmt.Fprintf(errOut, "Error: %s\n", e)
			}
			return &exitError{code: exitCode, err: fmt.Errorf("validation failed with %d error(s)", len(allErrors))}
		}

		if !flags.quiet {
			_, _ = fmt.Fprintln(out, "config and schema are valid")
		}
		return nil
	}
}
