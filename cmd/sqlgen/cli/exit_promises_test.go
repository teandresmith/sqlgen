package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/wrapper"
)

// tflSkipUnlessPermissionsBite skips a test that relies on a read-only
// directory refusing writes: Windows ignores the mode bits, and root
// bypasses them.
func tflSkipUnlessPermissionsBite(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("directory mode bits do not block writes on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
}

// tflReadOnlyDir makes dir read-only for the rest of the test and restores it
// before t.TempDir's cleanup removes it.
func tflReadOnlyDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0o500); err != nil { //nolint:gosec // test fixture: a directory that refuses writes
		t.Fatalf("making %s read-only: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // restores the temp dir so it can be removed
}

// tflStdinNotTTY points os.Stdin at the null device, so init takes its
// non-interactive path whatever terminal the test runs under.
func tflStdinNotTTY(t *testing.T) {
	t.Helper()
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("opening %s: %v", os.DevNull, err)
	}
	orig := os.Stdin
	os.Stdin = devNull
	t.Cleanup(func() {
		os.Stdin = orig
		_ = devNull.Close()
	})
}

// TestInit_InteractivePromptsForDialect pins "If run interactively (TTY
// detected), prompts for dialect choice". A test cannot hand the command a
// real terminal (detection is term.IsTerminal on stdin), so this drives the
// interactive branch the command takes when the check succeeds.
func TestInit_InteractivePromptsForDialect(t *testing.T) {
	tests := []struct {
		name        string
		answer      string
		wantDialect string
		wantDriver  string
	}{
		{name: "empty answer takes the default", answer: "\n", wantDialect: "postgres", wantDriver: "pgx"},
		{name: "numbered choice", answer: "2\n", wantDialect: "mysql", wantDriver: "stdlib"},
		{name: "named choice", answer: "sqlite\n", wantDialect: "sqlite", wantDriver: "stdlib"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			var out bytes.Buffer

			if err := runInit(&out, strings.NewReader(tt.answer), true); err != nil {
				t.Fatalf("runInit(interactive, %q): %v", tt.answer, err)
			}
			if !strings.Contains(out.String(), "Select a dialect:") || !strings.Contains(out.String(), "Enter choice [1]: ") {
				t.Errorf("interactive init did not prompt for the dialect; output:\n%s", out.String())
			}
			content, err := os.ReadFile("sqlgen.yml")
			if err != nil {
				t.Fatalf("reading sqlgen.yml: %v", err)
			}
			for _, want := range []string{"dialect: " + tt.wantDialect + "\n", "driver: " + tt.wantDriver + "\n"} {
				if !strings.Contains(string(content), want) {
					t.Errorf("answer %q wrote a config without %q:\n%s", tt.answer, want, content)
				}
			}
		})
	}
}

// TestInit_WriteErrorExitsGeneration pins that init failing to write
// sqlgen.yml is a file write error: exit 1, with the error on stderr.
func TestInit_WriteErrorExitsGeneration(t *testing.T) {
	tflSkipUnlessPermissionsBite(t)
	tflStdinNotTTY(t)
	dir := t.TempDir()
	t.Chdir(dir)
	tflReadOnlyDir(t, dir)

	code, _, stderr := tflRun("init")
	if code != ExitGeneration {
		t.Errorf("init in a read-only dir exit code = %d, want %d", code, ExitGeneration)
	}
	if !strings.Contains(stderr, "Error: writing sqlgen.yml: ") {
		t.Errorf("init stderr = %q, want an Error: line naming sqlgen.yml", stderr)
	}
}

// TestGraphQLInit_WritesScaffold pins that `sqlgen graphql init` scaffolds
// the starter gqlgen.yml at the configured path and exits 0.
func TestGraphQLInit_WritesScaffold(t *testing.T) {
	tests := []struct {
		name       string
		configured string // api.graphql.gqlgen_config; "" uses the default
		want       string // path, relative to the project root
	}{
		{name: "default path", configured: "", want: "gqlgen.yml"},
		{name: "configured path", configured: "./api/gqlgen.yml", want: filepath.Join("api", "gqlgen.yml")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgPath, _ := twrGraphQLProject(t, "CREATE TABLE products (id uuid PRIMARY KEY, name text NOT NULL);", "", tt.configured)
			if err := os.MkdirAll(filepath.Dir(tt.want), 0o750); err != nil {
				t.Fatalf("creating %s: %v", filepath.Dir(tt.want), err)
			}

			code, _, stderr := tflRun("graphql", "init", "--config", cfgPath)
			if code != ExitSuccess {
				t.Fatalf("graphql init exit code = %d, want 0; stderr:\n%s", code, stderr)
			}
			content, err := os.ReadFile(tt.want)
			if err != nil {
				t.Fatalf("graphql init did not write %s: %v", tt.want, err)
			}
			if string(content) != wrapper.InitConfigContent {
				t.Errorf("%s is not the starter scaffold:\n%s", tt.want, content)
			}
		})
	}
}

// TestGraphQLInit_UnwritableTargetExitsGeneration pins that `sqlgen graphql
// init` exits 1, printing the error, when the scaffold cannot be written.
func TestGraphQLInit_UnwritableTargetExitsGeneration(t *testing.T) {
	tflSkipUnlessPermissionsBite(t)
	cfgPath, _ := twrGraphQLProject(t, "CREATE TABLE products (id uuid PRIMARY KEY, name text NOT NULL);", "", "./locked/gqlgen.yml")
	if err := os.Mkdir("locked", 0o750); err != nil {
		t.Fatalf("creating locked dir: %v", err)
	}
	tflReadOnlyDir(t, "locked")

	code, _, stderr := tflRun("graphql", "init", "--config", cfgPath)
	if code != ExitGeneration {
		t.Errorf("graphql init exit code = %d, want %d", code, ExitGeneration)
	}
	if !strings.Contains(stderr, "Error: writing ./locked/gqlgen.yml: ") {
		t.Errorf("graphql init stderr = %q, want an Error: line naming the scaffold path", stderr)
	}
}

// TestExit_BareSchemaError pins that bare `sqlgen` exits 3 on a schema parse
// failure.
func TestExit_BareSchemaError(t *testing.T) {
	cfgPath, _ := tflProject(t, "CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL")

	code, _, stderr := tflRun("--config", cfgPath)
	if code != ExitSchema {
		t.Errorf("sqlgen exit code = %d, want %d; stderr:\n%s", code, ExitSchema, stderr)
	}
	if !strings.Contains(stderr, "Schema error: ") || !strings.Contains(stderr, "syntax error") {
		t.Errorf("sqlgen stderr = %q, want the schema syntax error", stderr)
	}
}

// TestExit_GraphQLGenSubprocessFailure pins that `sqlgen graphql gen` exits 1
// when the gqlgen step fails.
func TestExit_GraphQLGenSubprocessFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stand-in gqlgen is a POSIX shell script")
	}
	cfgPath, _ := twrGraphQLProject(t, "CREATE TABLE products (id uuid PRIMARY KEY, name text NOT NULL);", twrFollowSchemaGqlgenYML, "")
	appendConfig(t, cfgPath, "    gqlgen_bin: "+twrStandInGqlgen(t, "echo 'gqlgen: validation failed' >&2\nexit 1\n")+"\n")

	code, _, stderr := tflRun("graphql", "gen", "--config", cfgPath)
	if code != ExitGeneration {
		t.Errorf("graphql gen exit code = %d, want %d; stderr:\n%s", code, ExitGeneration, stderr)
	}
	if !strings.Contains(stderr, "Error: gqlgen subprocess failed") {
		t.Errorf("graphql gen stderr = %q, want the gqlgen failure", stderr)
	}
}

// TestExit_InvalidYAMLIsConfigError pins that invalid YAML in the config
// exits 2 under every command that reads it.
func TestExit_InvalidYAMLIsConfigError(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "bare", args: nil},
		{name: "generate", args: []string{"generate"}},
		{name: "validate", args: []string{"validate"}},
		{name: "diff", args: []string{"diff"}},
		{name: "lint", args: []string{"lint"}},
		{name: "graphql init", args: []string{"graphql", "init"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgPath, _ := tflProject(t, tflSchema)
			writeFileOrFail(t, cfgPath, "input: [unclosed\n")

			code, _, stderr := tflRun(append(tt.args, "--config", cfgPath)...)
			if code != ExitConfig {
				t.Errorf("sqlgen %v exit code = %d, want %d; stderr:\n%s", tt.args, code, ExitConfig, stderr)
			}
			if !strings.Contains(stderr, "Error: parsing config: yaml: ") {
				t.Errorf("sqlgen %v stderr = %q, want the YAML error", tt.args, stderr)
			}
		})
	}
}

// TestExit_TableDefinedTwiceIsSchemaError pins that a table defined twice in
// the schema exits 3.
func TestExit_TableDefinedTwiceIsSchemaError(t *testing.T) {
	tests := []struct {
		name    string
		command string
	}{
		{name: "generate", command: "generate"},
		{name: "validate", command: "validate"},
		{name: "diff", command: "diff"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgPath, _ := tflProject(t, "CREATE TABLE users (id serial PRIMARY KEY);\nCREATE TABLE users (id serial PRIMARY KEY);")

			code, _, stderr := tflRun(tt.command, "--config", cfgPath)
			if code != ExitSchema {
				t.Errorf("%s exit code = %d, want %d; stderr:\n%s", tt.command, code, ExitSchema, stderr)
			}
			if !strings.Contains(stderr, "table public.users already exists") {
				t.Errorf("%s stderr = %q, want the duplicate-table error", tt.command, stderr)
			}
		})
	}
}

// TestExit_AmbiguousConfigKeyIsConfigError pins that a bare config key naming
// a table present in more than one schema is a config error, exit 2, not a
// schema error.
func TestExit_AmbiguousConfigKeyIsConfigError(t *testing.T) {
	tests := []struct {
		name    string
		command string
	}{
		{name: "generate", command: "generate"},
		{name: "validate", command: "validate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgPath, _ := tflProject(t, "CREATE SCHEMA a;\nCREATE SCHEMA b;\nCREATE TABLE a.items (id serial PRIMARY KEY);\nCREATE TABLE b.items (id serial PRIMARY KEY);")
			appendConfig(t, cfgPath, "tables:\n  items:\n    struct_name: Item\n")

			code, _, stderr := tflRun(tt.command, "--config", cfgPath)
			if code != ExitConfig {
				t.Errorf("%s exit code = %d, want %d; stderr:\n%s", tt.command, code, ExitConfig, stderr)
			}
			if !strings.Contains(stderr, `ambiguous table reference "items": exists in schemas a, b`) {
				t.Errorf("%s stderr = %q, want the ambiguous-key error", tt.command, stderr)
			}
		})
	}
}

// TestExit_FileWriteErrorIsGenerationError pins that a file write error
// exits 1.
func TestExit_FileWriteErrorIsGenerationError(t *testing.T) {
	tests := []struct {
		name    string
		breakIt func(t *testing.T, outDir string)
	}{
		{
			name: "output dir is a regular file",
			breakIt: func(t *testing.T, outDir string) {
				writeFileOrFail(t, outDir, "not a directory\n")
			},
		},
		{
			name: "output dir is read-only",
			breakIt: func(t *testing.T, outDir string) {
				tflSkipUnlessPermissionsBite(t)
				if err := os.Mkdir(outDir, 0o750); err != nil {
					t.Fatalf("creating %s: %v", outDir, err)
				}
				tflReadOnlyDir(t, outDir)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgPath, outDir := tflProject(t, tflSchema)
			tt.breakIt(t, outDir)

			code, _, stderr := tflRun("generate", "--config", cfgPath)
			if code != ExitGeneration {
				t.Errorf("generate exit code = %d, want %d; stderr:\n%s", code, ExitGeneration, stderr)
			}
			if !strings.Contains(stderr, "Generation error: ") || !strings.Contains(stderr, outDir) {
				t.Errorf("generate stderr = %q, want a generation error naming %s", stderr, outDir)
			}
		})
	}
}

// TestExit_EveryFailurePrintsItsError pins "Every failing command prints its
// error to stderr, whatever the code": each failure below exits non-zero
// through execute, the binary's path, and names its error on stderr.
func TestExit_EveryFailurePrintsItsError(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T) []string // returns the arguments to run
		wantCode   int
		wantStderr string
	}{
		{
			name: "config error",
			setup: func(t *testing.T) []string {
				cfgPath, _ := tflProject(t, tflSchema)
				writeFileOrFail(t, cfgPath, "input: [unclosed\n")
				return []string{"generate", "--config", cfgPath}
			},
			wantCode:   ExitConfig,
			wantStderr: "Error: parsing config: ",
		},
		{
			name: "schema error",
			setup: func(t *testing.T) []string {
				cfgPath, _ := tflProject(t, "CREATE TABLE users (id serial PRIMARY KEY")
				return []string{"generate", "--config", cfgPath}
			},
			wantCode:   ExitSchema,
			wantStderr: "Schema error: ",
		},
		{
			name: "generation error",
			setup: func(t *testing.T) []string {
				return []string{"generate", "--config", ecrFieldCollisionProject(t)}
			},
			wantCode:   ExitGeneration,
			wantStderr: "is claimed by both",
		},
		{
			name: "usage error",
			setup: func(t *testing.T) []string {
				t.Chdir(t.TempDir())
				return []string{"generate", "--watch"}
			},
			wantCode:   ExitConfig,
			wantStderr: "Error: unknown flag: --watch",
		},
		{
			name: "lint issues",
			setup: func(t *testing.T) []string {
				cfgPath, _ := tflProject(t, "CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);")
				hooksDir := filepath.Join(filepath.Dir(cfgPath), "hooks")
				if err := os.Mkdir(hooksDir, 0o750); err != nil {
					t.Fatalf("creating hooks dir: %v", err)
				}
				// OpSoftDelete on a table without soft delete: a warning.
				writeFileOrFail(t, filepath.Join(hooksDir, "hooks.go"), `package hooks

import (
	"context"
	"github.com/teandresmith/sqlgen/hook"
)

var _ = hook.ForMutation[*CreateUserInput, *User](TableUsers, func(
	ctx context.Context,
	m *hook.MutationContext,
	input *CreateUserInput,
	next func(ctx context.Context) (*User, error),
) (*User, error) {
	if m.Op == hook.OpSoftDelete {
	}
	return next(ctx)
})
`)
				return []string{"lint", "--config", cfgPath, "--paths", hooksDir, "--fail-on", "warning"}
			},
			wantCode:   ExitGeneration,
			wantStderr: "Error: lint found issues at or above \"warning\" severity",
		},
		{
			name: "init refusal",
			setup: func(t *testing.T) []string {
				tflStdinNotTTY(t)
				t.Chdir(t.TempDir())
				writeFileOrFail(t, "sqlgen.yml", "input: {}\n")
				return []string{"init"}
			},
			wantCode:   ExitGeneration,
			wantStderr: "Error: sqlgen.yml already exists",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := tt.setup(t)

			code, _, stderr := tflRun(args...)
			if code != tt.wantCode {
				t.Errorf("sqlgen %v exit code = %d, want %d; stderr:\n%s", args, code, tt.wantCode, stderr)
			}
			if strings.TrimSpace(stderr) == "" || !strings.Contains(stderr, tt.wantStderr) {
				t.Errorf("sqlgen %v stderr = %q, want an error line containing %q", args, stderr, tt.wantStderr)
			}
		})
	}
}
