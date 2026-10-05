package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newCompletionCmd creates the completion subcommand that generates shell
// completion scripts for bash, zsh, fish, and powershell.
func newCompletionCmd() *cobra.Command {
	validShells := []string{"bash", "zsh", "fish", "powershell"}

	return &cobra.Command{
		Use:       "completion <shell>",
		Short:     "Generate shell completion scripts",
		Long:      "Generate shell completion scripts for bash, zsh, fish, or powershell.",
		Args:      cobra.ExactArgs(1),
		ValidArgs: validShells,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			root := cmd.Root()
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(out)
			case "zsh":
				return root.GenZshCompletion(out)
			case "fish":
				return root.GenFishCompletion(out, true)
			case "powershell":
				return root.GenPowerShellCompletion(out)
			default:
				return fmt.Errorf("unsupported shell: %q (valid: bash, zsh, fish, powershell)", args[0])
			}
		},
	}
}
