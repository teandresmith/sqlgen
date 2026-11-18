package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// tflSchema is a two-table schema, so a count of tables cannot be mistaken
// for a default of one.
const tflSchema = "CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);\nCREATE TABLE posts (id serial PRIMARY KEY, title text NOT NULL);"

// tflRun drives execute — the path the binary takes — and returns the exit
// code, stdout and stderr.
func tflRun(args ...string) (code int, stdout, stderr string) {
	rootCmd := NewRootCmd()
	var out, errOut bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errOut)
	rootCmd.SetArgs(args)
	code = execute(rootCmd)
	return code, out.String(), errOut.String()
}

// tflProject lays out a temp project holding ddl, chdirs into it, and returns
// the config path and the output directory.
func tflProject(t *testing.T, ddl string) (cfgPath, outDir string) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	cfgPath = filepath.Join(dir, "sqlgen.yml")
	outDir = filepath.Join(dir, "output")
	writeTestConfigWithDir(t, cfgPath, sqlDir, outDir)
	writeSQLFile(t, sqlDir, ddl)
	return cfgPath, outDir
}

// tflGenFiles returns the *_gen.go files in dir.
func tflGenFiles(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*_gen.go"))
	if err != nil {
		t.Fatalf("globbing %s: %v", dir, err)
	}
	if len(files) == 0 {
		t.Fatalf("generate wrote no *_gen.go files to %s", dir)
	}
	return files
}

// TestVerbose_ReportsTablesFilesAndTiming pins `--verbose` / `-v`: "Verbose
// output: show parsed tables, generated files, timing". The report replaces
// the one-line summary.
func TestVerbose_ReportsTablesFilesAndTiming(t *testing.T) {
	tests := []struct {
		name string
		flag string
	}{
		{name: "long flag", flag: "--verbose"},
		{name: "short flag", flag: "-v"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgPath, outDir := tflProject(t, tflSchema)

			code, stdout, stderr := tflRun("generate", tt.flag, "--config", cfgPath)
			if code != ExitSuccess {
				t.Fatalf("generate %s exit code = %d, want 0; stderr:\n%s", tt.flag, code, stderr)
			}
			lines := strings.Split(stdout, "\n")
			has := func(line string) bool {
				for _, l := range lines {
					if l == line {
						return true
					}
				}
				return false
			}

			// Parsed tables: the report counts them.
			if !has("  tables:  2") {
				t.Errorf("generate %s stdout lacks the parsed-table line %q:\n%s", tt.flag, "  tables:  2", stdout)
			}
			// Generated files: every file written is listed by path.
			for _, f := range tflGenFiles(t, outDir) {
				if !has("    " + f) {
					t.Errorf("generate %s stdout does not list generated file %s:\n%s", tt.flag, f, stdout)
				}
			}
			// Timing: an elapsed line carrying a parseable duration.
			elapsed := regexp.MustCompile(`(?m)^  elapsed: (\S+)$`).FindStringSubmatch(stdout)
			if elapsed == nil {
				t.Errorf("generate %s stdout lacks an elapsed line:\n%s", tt.flag, stdout)
			} else if _, err := time.ParseDuration(elapsed[1]); err != nil {
				t.Errorf("generate %s elapsed %q is not a duration: %v", tt.flag, elapsed[1], err)
			}
		})
	}
}

// TestVerbose_OffByDefault pins `--verbose` defaulting to false: without it,
// generate prints only its one-line summary — no table count, file listing
// or elapsed line.
func TestVerbose_OffByDefault(t *testing.T) {
	cfgPath, _ := tflProject(t, tflSchema)

	code, stdout, stderr := tflRun("generate", "--config", cfgPath)
	if code != ExitSuccess {
		t.Fatalf("generate exit code = %d, want 0; stderr:\n%s", code, stderr)
	}
	summary := regexp.MustCompile(`^sqlgen: generated \d+ files \(2 tables, 0 views\) in \S+\n$`)
	if !summary.MatchString(stdout) {
		t.Errorf("generate without -v stdout = %q, want only the summary line", stdout)
	}
}

// TestQuiet_SuppressesSummary pins `--quiet` / `-q`: "Suppress all output
// except errors". A successful run prints nothing.
func TestQuiet_SuppressesSummary(t *testing.T) {
	tests := []struct {
		name string
		flag string
	}{
		{name: "long flag", flag: "--quiet"},
		{name: "short flag", flag: "-q"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgPath, outDir := tflProject(t, tflSchema)

			code, stdout, stderr := tflRun("generate", tt.flag, "--config", cfgPath)
			if code != ExitSuccess {
				t.Fatalf("generate %s exit code = %d, want 0; stderr:\n%s", tt.flag, code, stderr)
			}
			tflGenFiles(t, outDir)
			if stdout != "" || stderr != "" {
				t.Errorf("generate %s printed output on success:\nstdout: %q\nstderr: %q", tt.flag, stdout, stderr)
			}
		})
	}
}

// TestQuiet_ErrorsStillPrint pins that `--quiet` suppresses all output
// *except errors*: a failing run under -q still names its error on stderr.
func TestQuiet_ErrorsStillPrint(t *testing.T) {
	tests := []struct {
		name       string
		ddl        string
		config     string // replaces sqlgen.yml when set
		extra      string // appended to sqlgen.yml
		wantCode   int
		wantStderr string
	}{
		{name: "config error", ddl: tflSchema, config: "input: [unclosed\n", wantCode: ExitConfig, wantStderr: "Error: parsing config"},
		{name: "schema error", ddl: "CREATE TABLE users (id serial PRIMARY KEY", wantCode: ExitSchema, wantStderr: "Schema error: "},
		{name: "generation error", ddl: "CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL, label text NOT NULL);", extra: "tables:\n  users:\n    column_map:\n      label:\n        name: Name\n", wantCode: ExitGeneration, wantStderr: "is claimed by both"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgPath, _ := tflProject(t, tt.ddl)
			if tt.config != "" {
				if err := os.WriteFile(cfgPath, []byte(tt.config), 0o600); err != nil {
					t.Fatalf("writing config: %v", err)
				}
			}
			if tt.extra != "" {
				appendConfig(t, cfgPath, tt.extra)
			}

			code, stdout, stderr := tflRun("generate", "-q", "--config", cfgPath)
			if code != tt.wantCode {
				t.Errorf("generate -q exit code = %d, want %d; stderr:\n%s", code, tt.wantCode, stderr)
			}
			if !strings.Contains(stderr, tt.wantStderr) {
				t.Errorf("generate -q stderr = %q, want it to contain %q", stderr, tt.wantStderr)
			}
			if stdout != "" {
				t.Errorf("generate -q stdout = %q, want empty", stdout)
			}
		})
	}
}

// TestConfigFlag_ShortForm pins `-c` as the short form of `--config`: run from
// a directory holding no config, `-c <path>` loads the named file.
func TestConfigFlag_ShortForm(t *testing.T) {
	cfgPath, outDir := tflProject(t, tflSchema)
	t.Chdir(t.TempDir())

	if code, _, stderr := tflRun("generate"); code != ExitConfig {
		t.Fatalf("generate with no config exit code = %d, want %d; the fixture must not be discoverable; stderr:\n%s", code, ExitConfig, stderr)
	}
	code, _, stderr := tflRun("generate", "-c", cfgPath)
	if code != ExitSuccess {
		t.Fatalf("generate -c %s exit code = %d, want 0; stderr:\n%s", cfgPath, code, stderr)
	}
	if _, err := os.Stat(filepath.Join(outDir, "user_gen.go")); err != nil {
		t.Errorf("generate -c did not generate from the named config: %v", err)
	}
}

// TestConfigDiscovery_ymlBeforeYaml pins the discovery order "search the
// current directory for `sqlgen.yml` then `sqlgen.yaml`": with both present
// the .yml is used, so a broken .yaml beside it is never read.
func TestConfigDiscovery_ymlBeforeYaml(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeTestConfig(t, filepath.Join(dir, "sqlgen.yml"))
	writeSQLFile(t, dir, "CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);")
	if err := os.WriteFile(filepath.Join(dir, "sqlgen.yaml"), []byte("input: [unclosed\n"), 0o600); err != nil {
		t.Fatalf("writing sqlgen.yaml: %v", err)
	}

	code, _, stderr := tflRun()
	if code != ExitSuccess {
		t.Fatalf("sqlgen with sqlgen.yml and a broken sqlgen.yaml exit code = %d, want 0 (sqlgen.yml used); stderr:\n%s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "output", "user_gen.go")); err != nil {
		t.Errorf("sqlgen did not generate from sqlgen.yml: %v", err)
	}
}
