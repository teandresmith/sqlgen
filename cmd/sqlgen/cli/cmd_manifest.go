package cli

import (
	"github.com/spf13/cobra"
)

// Manifest subcommand exit codes (PRD §30.8). These are intentionally distinct
// from the top-level generation exit codes in root.go: the manifest tools are
// CI-oriented and follow the git-style "0 = clean, 1 = differences/failure"
// convention.
//
//	validate: 0 success, 1 schema-validation failure, 2 I/O failure.
//	diff:     0 no differences, 1 differences found. I/O failures also surface
//	          as 2 for symmetry with validate.
const (
	manifestExitClean  = 0
	manifestExitFail   = 1
	manifestExitIOFail = 2
)

// newManifestCmd builds the `sqlgen manifest` parent command and its
// `validate` / `diff` subcommands. Both operate purely on the on-disk manifest
// JSON with no runtime dependency (PRD §30.8).
func newManifestCmd(flags *cliFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "manifest",
		Short: "Inspect generated manifest artifacts (validate / diff)",
	}
	cmd.AddCommand(newManifestValidateCmd(flags))
	cmd.AddCommand(newManifestDiffCmd(flags))
	return cmd
}
