package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidate_reachesGenerationPhase pins that validate runs generation-phase
// checks.
// `validate` used to stop after ValidatePostParse, so a config that only
// `generate` rejects printed "config and schema are valid" and then failed on
// the next generate. Both shapes of the resolved-field-name rule must now be
// visible from validate: a rename onto a name the templates already declare,
// and a plain column whose derived name does the same.
func TestValidate_reachesGenerationPhase(t *testing.T) {
	tests := []struct {
		name    string
		schema  string
		table   string
		wantErr string
	}{
		{
			name:    "renamed onto a reserved name",
			schema:  "CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);",
			table:   "\ntables:\n  users:\n    column_map:\n      name:\n        name: And\n",
			wantErr: `resolves to Go field name "And"`,
		},
		{
			name:    "plain column name is reserved",
			schema:  "CREATE TABLE users (id serial PRIMARY KEY, columns text NOT NULL);",
			wantErr: `resolves to Go field name "Columns"`,
		},
		{
			name:    "duplicate resolved name",
			schema:  "CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL, label text NOT NULL);",
			table:   "\ntables:\n  users:\n    column_map:\n      label:\n        name: Name\n",
			wantErr: `is claimed by both`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "sqlgen.yml")
			sqlDir := filepath.Join(dir, "migrations")
			if err := os.MkdirAll(sqlDir, 0o750); err != nil {
				t.Fatalf("creating migrations dir: %v", err)
			}
			outputDir := filepath.Join(dir, "output")
			writeValidateConfig(t, cfgPath, sqlDir, outputDir, tt.table)
			writeSQLFile(t, sqlDir, tt.schema)

			stdout, stderr, err := executeCommand("validate", "--config", cfgPath)
			if err == nil {
				t.Fatalf("validate returned nil error; stdout = %q", stdout)
			}

			var ee *exitError
			if !errors.As(err, &ee) || ee.code != ExitConfig {
				t.Errorf("expected exit code %d (config), got error: %v", ExitConfig, err)
			}
			if !strings.Contains(stderr, tt.wantErr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, tt.wantErr)
			}
			if strings.Contains(stdout, "config and schema are valid") {
				t.Error("validate reported the config valid while generate would reject it")
			}
			// The generation-phase check must not turn validate into a
			// generator: nothing is written.
			if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
				t.Error("validate created the output directory")
			}
		})
	}
}

// writeValidateConfig writes the standard postgres test config with an extra
// YAML section appended, so a case can add the `tables:` block it needs.
func writeValidateConfig(t *testing.T, path, sqlDir, outputDir, extra string) {
	t.Helper()
	content := `input:
  dialect: postgres
  paths:
    - "` + sqlDir + `"
output:
  driver: pgx
  dir: ` + outputDir + `
  package: models
  layout: file_per_table
` + extra
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
}
