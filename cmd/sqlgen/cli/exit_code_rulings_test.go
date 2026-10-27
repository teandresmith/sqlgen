package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ecrFieldCollisionProject writes a project whose two columns resolve to one
// Go field: column_map renames label onto name's field, Name. Config loading
// and both config validation passes accept it; only resolving the generation
// contexts rejects it. It returns the config path.
func ecrFieldCollisionProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	writeValidateConfig(t, cfgPath, sqlDir, filepath.Join(dir, "output"),
		"\ntables:\n  users:\n    column_map:\n      label:\n        name: Name\n")
	writeSQLFile(t, sqlDir, "CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL, label text NOT NULL);")
	return cfgPath
}

// TestExitCode_ResolutionErrorIsGenerationFailure pins that an error found
// while resolving the generation contexts exits 1 (generation failure) under
// every command that resolves them, rather than 2 under validate alone.
func TestExitCode_ResolutionErrorIsGenerationFailure(t *testing.T) {
	tests := []struct {
		name    string
		command string
	}{
		{name: "validate", command: "validate"},
		{name: "generate", command: "generate"},
		{name: "lint", command: "lint"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgPath := ecrFieldCollisionProject(t)

			_, stderr, err := executeCommand(tt.command, "--config", cfgPath)
			if code := exitCodeOf(t, err); code != ExitGeneration {
				t.Errorf("%s exit code = %d, want %d (generation); err = %v", tt.command, code, ExitGeneration, err)
			}
			if !strings.Contains(stderr, "is claimed by both") {
				t.Errorf("%s stderr = %q, want the field collision error", tt.command, stderr)
			}
		})
	}
}
