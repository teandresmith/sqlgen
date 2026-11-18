// Package cli implements the sqlgen command-line interface. It provides the
// root command, subcommands, and shared pipeline helpers for config loading,
// schema parsing, and code generation.
package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"
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
func Execute() int { return execute(NewRootCmd()) }

// execute runs rootCmd and maps its error to the process exit code. An
// *exitError was already printed by the command that returned it; any other
// error is printed here, and a *usageError exits ExitConfig.
func execute(rootCmd *cobra.Command) int {
	err := rootCmd.Execute()
	if err == nil {
		return ExitSuccess
	}
	if code, ok := exitCodeFromError(err); ok {
		return code
	}
	_, _ = fmt.Fprintf(rootCmd.ErrOrStderr(), "Error: %v\n", err)
	if _, ok := errors.AsType[*usageError](err); ok {
		return ExitConfig
	}
	return ExitGeneration
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
		// A positional argument on the root is a mistyped subcommand.
		Args: rejectUnknownSubcommand,
	}
	// Subcommands inherit the root's flag error func.
	rootCmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &usageError{err: err}
	})

	rootCmd.PersistentFlags().StringVarP(&flags.config, "config", "c", "", "path to config file (default: sqlgen.yml or sqlgen.yaml)")
	rootCmd.PersistentFlags().BoolVarP(&flags.verbose, "verbose", "v", false, "verbose output: show parsed tables, generated files, timing")
	rootCmd.PersistentFlags().BoolVarP(&flags.quiet, "quiet", "q", false, "suppress all output except errors")
	// The only error is an undefined flag name; config is defined above.
	_ = rootCmd.MarkPersistentFlagFilename("config", "yml", "yaml")

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
		Args:  usageArgs(cobra.NoArgs),
		Short: "Run code generation using the config file",
		RunE:  generateRun,
	}
	generateSubCmd.Flags().BoolVar(&flags.noGraphQL, "no-graphql", false, "skip the chained 'sqlgen graphql gen' invocation when api.graphql.enabled is true")
	generateSubCmd.Flags().StringVar(&flags.manifestTimestamp, "manifest-timestamp", "", "RFC3339 timestamp for the manifest generated_at field (default: current UTC time)")
	rootCmd.AddCommand(generateSubCmd)

	rootCmd.AddCommand(&cobra.Command{
		Use:   "init",
		Args:  usageArgs(cobra.NoArgs),
		Short: "Generate a starter sqlgen.yml config file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// A character device such as /dev/null is not a terminal.
			interactive := term.IsTerminal(int(os.Stdin.Fd())) //nolint:gosec // a file descriptor fits in an int.
			return runInit(cmd.OutOrStdout(), cmd.InOrStdin(), interactive)
		},
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "validate",
		Args:  usageArgs(cobra.NoArgs),
		Short: "Validate config and schema without generating code",
		RunE:  makeValidateRunE(flags),
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "diff",
		Args:  usageArgs(cobra.NoArgs),
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

// usageError marks a command-line usage mistake: an unknown command or flag,
// a wrong argument count, or an invalid argument or flag value. execute prints
// it and exits ExitConfig.
type usageError struct {
	err error
}

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

// rejectUnknownSubcommand is the Args check of a command whose positional
// arguments can only be a mistyped subcommand.
func rejectUnknownSubcommand(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	return &usageError{err: fmt.Errorf("unknown command %q for %q", args[0], cmd.CommandPath())}
}

// asParent makes a command that only groups subcommands print its help when
// run alone, and reject an unknown subcommand as a usage error. Without a
// RunE, cobra prints the help for `sqlgen graphql nosuch` and exits 0.
func asParent(cmd *cobra.Command) *cobra.Command {
	cmd.Args = rejectUnknownSubcommand
	cmd.RunE = func(cmd *cobra.Command, _ []string) error { return cmd.Help() }
	return cmd
}

// usageArgs wraps a positional-argument validator so its error is a
// *usageError.
func usageArgs(validate cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := validate(cmd, args); err != nil {
			return &usageError{err: err}
		}
		return nil
	}
}

// exitCodeFromError extracts an exit code from an error, if it's an *exitError.
func exitCodeFromError(err error) (int, bool) {
	if ee, ok := errors.AsType[*exitError](err); ok {
		return ee.code, true
	}
	return 0, false
}
