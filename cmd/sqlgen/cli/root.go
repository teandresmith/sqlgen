// Package cli implements the sqlgen command-line interface. It provides the
// root command, subcommands, and shared pipeline helpers for config loading,
// schema parsing, and code generation.
package cli

import (
	"errors"
	"os"

	"github.com/spf13/cobra"
)

// Exit codes per PRD Section 23.9.
const (
	ExitSuccess    = 0
	ExitGeneration = 1
	ExitConfig     = 2
	ExitSchema     = 3
	ExitConnection = 4
)

// cliFlags holds flag values for the CLI session.
type cliFlags struct {
	config            string
	verbose           bool
	quiet             bool
	noGraphQL         bool
	manifestTimestamp string
}

// Execute constructs and runs the root command, returning the process exit code.
func Execute() int {
	rootCmd := NewRootCmd()
	if err := rootCmd.Execute(); err != nil {
		if code, ok := exitCodeFromError(err); ok {
			return code
		}
		return ExitGeneration
	}
	return ExitSuccess
}

// NewRootCmd creates the root cobra command with all subcommands.
func NewRootCmd() *cobra.Command {
	flags := &cliFlags{}
	generateRun := makeGenerateRunE(flags)

	rootCmd := &cobra.Command{
		Use:           "sqlgen",
		Short:         "Type-safe SQL-to-Go code generator",
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       Version,
		// Make `generate` the default command when no subcommand is given.
		RunE: generateRun,
	}

	rootCmd.PersistentFlags().StringVarP(&flags.config, "config", "c", "", "path to config file (default: sqlgen.yml or sqlgen.yaml)")
	rootCmd.PersistentFlags().BoolVarP(&flags.verbose, "verbose", "v", false, "verbose output: show parsed tables, generated files, timing")
	rootCmd.PersistentFlags().BoolVarP(&flags.quiet, "quiet", "q", false, "suppress all output except errors")

	// --no-graphql suppresses the chained `sqlgen graphql gen`
	// invocation that runs after generation when api.graphql.enabled is
	// true. SQLGEN_NO_GRAPHQL=1 has the same effect for environments where
	// gqlgen runs in a separate CI step.
	rootCmd.Flags().BoolVar(&flags.noGraphQL, "no-graphql", false, "skip the chained 'sqlgen graphql gen' invocation when api.graphql.enabled is true")
	// --manifest-timestamp pins the manifest's generated_at field (PRD §30.3
	// Determinism). Unset falls back to the current UTC time; set it to a fixed
	// RFC3339 value for byte-deterministic manifest output (e.g. golden tests).
	rootCmd.Flags().StringVar(&flags.manifestTimestamp, "manifest-timestamp", "", "RFC3339 timestamp for the manifest generated_at field (default: current UTC time)")

	rootCmd.SetVersionTemplate("sqlgen {{.Version}}\n")

	generateSubCmd := &cobra.Command{
		Use:   "generate",
		Short: "Run code generation using the config file",
		RunE:  generateRun,
	}
	generateSubCmd.Flags().BoolVar(&flags.noGraphQL, "no-graphql", false, "skip the chained 'sqlgen graphql gen' invocation when api.graphql.enabled is true")
	generateSubCmd.Flags().StringVar(&flags.manifestTimestamp, "manifest-timestamp", "", "RFC3339 timestamp for the manifest generated_at field (default: current UTC time)")
	rootCmd.AddCommand(generateSubCmd)

	rootCmd.AddCommand(&cobra.Command{
		Use:   "init",
		Short: "Generate a starter sqlgen.yml config file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fi, err := os.Stdin.Stat()
			interactive := err == nil && fi.Mode()&os.ModeCharDevice != 0
			return runInit(cmd.OutOrStdout(), cmd.InOrStdin(), interactive)
		},
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "validate",
		Short: "Validate config and schema without generating code",
		RunE:  makeValidateRunE(flags),
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "diff",
		Short: "Preview what files would be created, modified, or deleted",
		RunE:  makeDiffRunE(flags),
	})

	rootCmd.AddCommand(newCompletionCmd())
	rootCmd.AddCommand(newLintCmd(flags))
	rootCmd.AddCommand(newGraphQLCmd(flags))
	rootCmd.AddCommand(newManifestCmd(flags))
	rootCmd.AddCommand(newMCPCmd(flags))

	return rootCmd
}

// exitError wraps an error with an exit code.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

// exitCodeFromError extracts an exit code from an error, if it's an *exitError.
func exitCodeFromError(err error) (int, bool) {
	if ee, ok := errors.AsType[*exitError](err); ok {
		return ee.code, true
	}
	return 0, false
}
