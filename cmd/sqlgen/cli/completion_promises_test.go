package cli

import (
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestCompletion_PrintsEachShellsScript pins that `sqlgen completion <shell>`
// prints that shell's completion script and exits 0. Each marker is the line
// cobra's generator for that shell emits to register the script with the
// shell, so a script for the wrong shell fails.
func TestCompletion_PrintsEachShellsScript(t *testing.T) {
	tests := []struct {
		name   string
		marker string
	}{
		{name: "bash", marker: "complete -o default -F __start_sqlgen sqlgen"},
		{name: "zsh", marker: "#compdef sqlgen\n"},
		{name: "fish", marker: "complete -c sqlgen -e\n"},
		{name: "powershell", marker: "Register-ArgumentCompleter -CommandName 'sqlgen'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := tflRun("completion", tt.name)
			if code != ExitSuccess {
				t.Fatalf("completion %s exit code = %d, want 0; stderr:\n%s", tt.name, code, stderr)
			}
			if !strings.Contains(stdout, tt.marker) {
				t.Errorf("completion %s output lacks the %s registration %q; got %d bytes starting:\n%.300s", tt.name, tt.name, tt.marker, len(stdout), stdout)
			}
			if tt.name == "zsh" && !strings.HasPrefix(stdout, tt.marker) {
				t.Errorf("completion zsh output must start with %q; got:\n%.100s", tt.marker, stdout)
			}
		})
	}
}

// tflCompletionValues returns the candidate values (descriptions stripped)
// cobra's __complete offers for args.
func tflCompletionValues(t *testing.T, args ...string) map[string]bool {
	t.Helper()
	candidates, _ := cspCompletionLines(t, args...)
	got := map[string]bool{}
	for _, c := range candidates {
		value, _, _ := strings.Cut(c, "\t")
		got[value] = true
	}
	return got
}

// TestCompletion_CoversAllCommands pins "Completions cover all commands":
// completing a command word offers every subcommand of that level, checked
// against both a fixed list and the live command tree so a command added
// later is covered too.
func TestCompletion_CoversAllCommands(t *testing.T) {
	tests := []struct {
		name string
		path []string
		want []string
	}{
		{name: "top level", path: nil, want: []string{"completion", "diff", "generate", "graphql", "init", "lint", "manifest", "mcp", "validate"}},
		{name: "graphql", path: []string{"graphql"}, want: []string{"init", "gen"}},
		{name: "manifest", path: []string{"manifest"}, want: []string{"validate", "diff"}},
		{name: "mcp", path: []string{"mcp"}, want: []string{"serve"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tflCompletionValues(t, append(append([]string{}, tt.path...), "")...)

			want := append([]string{}, tt.want...)
			node, _, err := NewRootCmd().Find(tt.path)
			if err != nil {
				t.Fatalf("finding %v in the command tree: %v", tt.path, err)
			}
			for _, sub := range node.Commands() {
				if sub.IsAvailableCommand() {
					want = append(want, sub.Name())
				}
			}
			for _, name := range want {
				if !got[name] {
					t.Errorf("__complete %v offers %v, missing command %q", tt.path, got, name)
				}
			}
		})
	}
}

// TestCompletion_CoversAllFlags pins "Completions cover all flags":
// completing `-` after a command offers every flag that command accepts, its
// own and the inherited persistent ones, in long and short form.
func TestCompletion_CoversAllFlags(t *testing.T) {
	tests := []struct {
		name string
		path []string
		want []string // a fixed floor, so a tree lookup that found nothing still fails
	}{
		{name: "root", path: nil, want: []string{"--config", "-c", "--verbose", "-v", "--quiet", "-q", "--no-graphql", "--manifest-timestamp"}},
		{name: "generate", path: []string{"generate"}, want: []string{"--config", "-c", "--verbose", "-v", "--quiet", "-q", "--no-graphql", "--manifest-timestamp"}},
		{name: "validate", path: []string{"validate"}, want: []string{"--config", "--verbose", "--quiet"}},
		{name: "diff", path: []string{"diff"}, want: []string{"--config", "--verbose", "--quiet"}},
		{name: "lint", path: []string{"lint"}, want: []string{"--paths", "--fail-on", "--config"}},
		{name: "graphql init", path: []string{"graphql", "init"}, want: []string{"--config"}},
		{name: "graphql gen", path: []string{"graphql", "gen"}, want: []string{"--config"}},
		{name: "manifest validate", path: []string{"manifest", "validate"}, want: []string{"--config"}},
		{name: "manifest diff", path: []string{"manifest", "diff"}, want: []string{"--json"}},
		{name: "mcp serve", path: []string{"mcp", "serve"}, want: []string{"--manifest", "--stdio", "--http", "--watch", "--no-watch", "--log", "--log-level"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tflCompletionValues(t, append(append([]string{}, tt.path...), "-")...)

			cmd, _, err := NewRootCmd().Find(tt.path)
			if err != nil {
				t.Fatalf("finding %v in the command tree: %v", tt.path, err)
			}
			want := append([]string{}, tt.want...)
			want = append(want, tflFlagNames(cmd)...)
			for _, name := range want {
				if !got[name] {
					t.Errorf("__complete %v - offers %v, missing flag %q", tt.path, got, name)
				}
			}
		})
	}
}

// tflFlagUsage matches one flag line of pflag's usage text, e.g.
// "  -c, --config string   path ..." or "      --no-graphql   skip ...".
var tflFlagUsage = regexp.MustCompile(`(?m)^\s+(?:-(\w), )?--([\w-]+)`)

// tflFlagNames returns every visible flag cmd accepts, its own and the
// inherited ones, as --name and -s. It reads the usage text, which already
// leaves hidden and deprecated flags out.
func tflFlagNames(cmd *cobra.Command) []string {
	var names []string
	usages := cmd.LocalFlags().FlagUsages() + cmd.InheritedFlags().FlagUsages()
	for _, m := range tflFlagUsage.FindAllStringSubmatch(usages, -1) {
		names = append(names, "--"+m[2])
		if m[1] != "" {
			names = append(names, "-"+m[1])
		}
	}
	return names
}
