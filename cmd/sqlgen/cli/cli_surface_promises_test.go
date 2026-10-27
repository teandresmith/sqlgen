package cli

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

// cspCompletionLines runs cobra's hidden __complete command and returns the
// candidate lines and the trailing ":<directive>" line.
func cspCompletionLines(t *testing.T, args ...string) (candidates []string, directive string) {
	t.Helper()
	stdout, _, err := executeCommand(append([]string{"__complete"}, args...)...)
	if err != nil {
		t.Fatalf("__complete %v: %v", args, err)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(stdout), "\n") {
		if strings.HasPrefix(line, ":") {
			directive = line
			continue
		}
		candidates = append(candidates, line)
	}
	return candidates, directive
}

// TestCompletion_ConfigFlagCompletesYAMLFiles pins "`--config` completes
// `.yml`/`.yaml` files": the completion filters files by those two
// extensions rather than offering every file.
func TestCompletion_ConfigFlagCompletesYAMLFiles(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "long flag", args: []string{"--config", ""}},
		{name: "short flag", args: []string{"-c", ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates, directive := cspCompletionLines(t, tt.args...)
			// ShellCompDirectiveFilterFileExt is 8; the candidates are the extensions.
			if directive != ":8" {
				t.Errorf("directive = %q, want :8 (filter by file extension)", directive)
			}
			got := strings.Join(candidates, ",")
			if got != "yml,yaml" && got != "yaml,yml" {
				t.Errorf("extensions = %q, want yml and yaml", got)
			}
		})
	}
}

// TestCompletion_FailOnFlagCompletesSeverities pins "Completions cover ...
// flag values" for the one enumerated flag value set, `lint --fail-on`.
func TestCompletion_FailOnFlagCompletesSeverities(t *testing.T) {
	candidates, _ := cspCompletionLines(t, "lint", "--fail-on", "")
	got := map[string]bool{}
	for _, c := range candidates {
		value, _, _ := strings.Cut(c, "\t")
		got[value] = true
	}
	for _, want := range []string{"error", "warning", "info"} {
		if !got[want] {
			t.Errorf("--fail-on completions %q lack %q", candidates, want)
		}
	}
}

// TestInit_NonTTYStdinDoesNotPrompt pins "If run interactively (TTY
// detected), prompts for dialect choice. Otherwise uses `postgres`": stdin
// redirected from /dev/null is not a terminal, so init must not prompt.
func TestInit_NonTTYStdinDoesNotPrompt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("/dev/null is POSIX")
	}
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("opening %s: %v", os.DevNull, err)
	}
	t.Cleanup(func() { _ = devNull.Close() })
	orig := os.Stdin
	os.Stdin = devNull
	t.Cleanup(func() { os.Stdin = orig })
	t.Chdir(t.TempDir())

	stdout, _, err := executeCommand("init")
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if strings.Contains(stdout, "Select a dialect") {
		t.Errorf("init prompted with stdin from %s, which is not a TTY:\n%s", os.DevNull, stdout)
	}
	content, err := os.ReadFile("sqlgen.yml")
	if err != nil {
		t.Fatalf("reading sqlgen.yml: %v", err)
	}
	if !strings.Contains(string(content), "dialect: postgres") {
		t.Errorf("non-interactive init wrote no postgres dialect:\n%s", content)
	}
}

// TestGraphQLInit_MissingConfigPrintsInitHint pins config discovery step 3,
// "If neither exists, print an error suggesting `sqlgen init`", for
// `sqlgen graphql init`, which reads the config to find gqlgen_config.
func TestGraphQLInit_MissingConfigPrintsInitHint(t *testing.T) {
	t.Chdir(t.TempDir())

	_, stderr, err := executeCommand("graphql", "init")
	if code := exitCodeOf(t, err); code != ExitConfig {
		t.Errorf("exit code = %d, want %d", code, ExitConfig)
	}
	if !strings.Contains(stderr, "sqlgen init") {
		t.Errorf("stderr = %q, want an error suggesting sqlgen init", stderr)
	}
}

// TestQuiet_SuppressesGqlgenStdout pins `--quiet`: "Suppress all output
// except errors". The chained gqlgen step's stdout is not an error.
func TestQuiet_SuppressesGqlgenStdout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stand-in gqlgen is a POSIX shell script")
	}
	const marker = "STDOUT-FROM-GQLGEN"
	tests := []struct {
		name string
		args []string
	}{
		{name: "generate", args: []string{"generate"}},
		{name: "graphql gen", args: []string{"graphql", "gen"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgPath, _ := twrGraphQLProject(t, "CREATE TABLE products (id uuid PRIMARY KEY, name text NOT NULL);", twrFollowSchemaGqlgenYML, "")
			appendConfig(t, cfgPath, "    gqlgen_bin: "+twrStandInGqlgen(t, "echo '"+marker+"'\n")+"\n")

			// Control: without -q the subprocess's stdout reaches stdout, so
			// its absence below is the flag's doing.
			stdout, stderr, err := executeCommand(append(tt.args, "--config", cfgPath)...)
			if err != nil {
				t.Fatalf("%v without -q: %v\nstderr: %s", tt.args, err, stderr)
			}
			if !strings.Contains(stdout, marker) {
				t.Fatalf("%v without -q stdout lacks the gqlgen subprocess's output %q:\n%s", tt.args, marker, stdout)
			}

			stdout, stderr, err = executeCommand(append(tt.args, "-q", "--config", cfgPath)...)
			if err != nil {
				t.Fatalf("%v -q: %v\nstderr: %s", tt.args, err, stderr)
			}
			if strings.Contains(stdout, marker) {
				t.Errorf("-q stdout carries the gqlgen subprocess's output:\n%s", stdout)
			}
		})
	}
}
