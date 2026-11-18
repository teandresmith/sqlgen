package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGenerate_ManifestTimestampMustBeRFC3339 pins PRD §30.4: `generated_at`
// is an RFC3339 timestamp, and --manifest-timestamp only pins it (§30.3). A
// flag value that is not RFC3339 is a usage error: exit 2, stderr naming the
// flag and the RFC3339 form it wants, and nothing generated — the check runs
// before any file is written. Both entry points that take the flag (the root
// command's default generate and `generate`) refuse it the same way.
func TestGenerate_ManifestTimestampMustBeRFC3339(t *testing.T) {
	tests := []struct {
		name string
		flag string
	}{
		{name: "a date with no time", flag: "2026-07-09"},
		{name: "a word", flag: "yesterday"},
		{name: "a time with no offset", flag: "2026-07-09T12:34:56"},
	}
	for _, entry := range []struct {
		name string
		args []string
	}{
		{name: "root", args: nil},
		{name: "generate", args: []string{"generate"}},
	} {
		for _, tt := range tests {
			t.Run(entry.name+"/"+tt.name, func(t *testing.T) {
				cfgPath, outDir := mtSQLiteProject(t)
				args := append(append([]string{}, entry.args...), "--config", cfgPath, "--manifest-timestamp", tt.flag)

				rootCmd := NewRootCmd()
				var stdout, stderr bytes.Buffer
				rootCmd.SetOut(&stdout)
				rootCmd.SetErr(&stderr)
				rootCmd.SetArgs(args)
				code := execute(rootCmd)

				if code != ExitConfig {
					t.Errorf("exit code = %d, want %d (usage error)\nstderr: %s", code, ExitConfig, stderr.String())
				}
				want := `Error: invalid --manifest-timestamp "` + tt.flag + `": want an RFC3339 timestamp such as 2026-07-09T12:34:56Z`
				if got := strings.TrimSpace(stderr.String()); got != want {
					t.Errorf("stderr = %q, want %q", got, want)
				}
				if _, err := os.Stat(outDir); !os.IsNotExist(err) {
					t.Errorf("output dir %s exists after a refused run (stat err: %v); nothing may be generated", outDir, err)
				}
			})
		}
	}
}

// mtSQLiteProject writes a one-table sqlite project with the manifest enabled
// and returns its config path and (not yet created) output directory.
func mtSQLiteProject(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	sqlDir := filepath.Join(dir, "migrations")
	outDir := filepath.Join(dir, "models")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatal(err)
	}
	writeSQLFile(t, sqlDir, "CREATE TABLE products (id integer PRIMARY KEY, name text NOT NULL);")
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	body := "version: v1\n" +
		"input:\n  dialect: sqlite\n  paths:\n    - " + filepath.ToSlash(sqlDir) + "\n" +
		"output:\n  driver: stdlib\n  dir: " + filepath.ToSlash(outDir) + "\n  package: models\n" +
		"generation:\n  manifest:\n    enabled: true\n    mcp:\n      emit_project_config: false\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfgPath, outDir
}
