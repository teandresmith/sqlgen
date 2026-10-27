package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// useRun drives execute — the path the binary takes — and returns the exit
// code and stderr.
func useRun(args ...string) (code int, stderr string) {
	rootCmd := NewRootCmd()
	var out, errOut bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errOut)
	rootCmd.SetArgs(args)
	code = execute(rootCmd)
	return code, errOut.String()
}

// TestExecute_UsageErrorsExitConfigAndPrint pins that a usage error — an
// unknown command or flag, a wrong argument count, an invalid argument or
// flag value — exits 2 and names the problem on stderr.
func TestExecute_UsageErrorsExitConfigAndPrint(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "unknown flag on generate", args: []string{"generate", "--watch"}},
		{name: "flag validate does not define", args: []string{"validate", "--no-graphql"}},
		{name: "invalid fail-on value", args: []string{"lint", "--fail-on", "bogus"}},
		{name: "unsupported completion shell", args: []string{"completion", "nushell"}},
		{name: "completion without a shell", args: []string{"completion"}},
		{name: "unknown command", args: []string{"nosuchcommand"}},
		{name: "unknown subcommand of graphql", args: []string{"graphql", "nosuch"}},
		{name: "unknown subcommand of manifest", args: []string{"manifest", "nosuch"}},
		{name: "unknown subcommand of mcp", args: []string{"mcp", "nosuch"}},
		{name: "mutually exclusive watch flags", args: []string{"mcp", "serve", "--watch", "--no-watch"}},
		{name: "unknown flag on validate", args: []string{"validate", "--watch"}},
		{name: "unknown flag on diff", args: []string{"diff", "--watch"}},
		{name: "unknown flag on lint", args: []string{"lint", "--watch"}},
		{name: "extra argument to generate", args: []string{"generate", "extra"}},
		{name: "extra argument to init", args: []string{"init", "extra"}},
		{name: "extra argument to validate", args: []string{"validate", "extra"}},
		{name: "extra argument to diff", args: []string{"diff", "extra"}},
		{name: "extra argument to lint", args: []string{"lint", "extra"}},
		{name: "extra argument to graphql init", args: []string{"graphql", "init", "extra"}},
		{name: "extra argument to graphql gen", args: []string{"graphql", "gen", "extra"}},
		{name: "mcp serve with no transport", args: []string{"mcp", "serve", "--stdio=false"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())

			code, stderr := useRun(tt.args...)
			if code != ExitConfig {
				t.Errorf("sqlgen %v exit code = %d, want %d (usage); stderr = %q", tt.args, code, ExitConfig, stderr)
			}
			if n := strings.Count(stderr, "Error: "); n != 1 {
				t.Errorf("sqlgen %v stderr = %q, want exactly one Error: line, got %d", tt.args, stderr, n)
			}
		})
	}
}

// TestExecute_InitOverExistingConfigPrintsError pins that init refusing to
// overwrite sqlgen.yml exits 1 and says why.
func TestExecute_InitOverExistingConfigPrintsError(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("sqlgen.yml", []byte("input: {}\n"), 0o600); err != nil {
		t.Fatalf("writing sqlgen.yml: %v", err)
	}

	code, stderr := useRun("init")
	if code != ExitGeneration {
		t.Errorf("init exit code = %d, want %d", code, ExitGeneration)
	}
	if !strings.Contains(stderr, "Error: ") || !strings.Contains(stderr, "sqlgen.yml already exists") {
		t.Errorf("init stderr = %q, want an Error: line naming the existing sqlgen.yml", stderr)
	}
}
