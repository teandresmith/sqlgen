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

// validate builds the generation contexts even after the config rules fail,
// so a relationship with no `name:` (required, PRD §4.8) must yield only the
// config error: no panic for an O2O edge, and no api-context error for an
// O2M edge's filter member.
func TestValidate_relationshipWithoutName(t *testing.T) {
	const schema = `CREATE TABLE owners (id serial PRIMARY KEY);
CREATE TABLE owner_profiles (id serial PRIMARY KEY, owner_id int NOT NULL UNIQUE REFERENCES owners(id), bio text);
CREATE TABLE tags (id serial PRIMARY KEY);
CREATE TABLE owner_tags (owner_id int NOT NULL REFERENCES owners(id), tag_id int NOT NULL REFERENCES tags(id), PRIMARY KEY (owner_id, tag_id));`
	tests := []struct {
		name string
		rel  string
	}{
		{name: "o2o", rel: "type: o2o\n        table: owner_profiles\n        fk: owner_id\n"},
		{name: "o2m", rel: "type: o2m\n        table: owner_profiles\n        fk: owner_id\n        filter: \"bio IS NOT NULL\"\n"},
		{name: "m2m", rel: "type: m2m\n        table: tags\n        junction: owner_tags\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "sqlgen.yml")
			sqlDir := filepath.Join(dir, "migrations")
			if err := os.MkdirAll(sqlDir, 0o750); err != nil {
				t.Fatalf("creating migrations dir: %v", err)
			}
			// The api block builds the filter members a nameless edge would
			// render only half of.
			writeValidateConfig(t, cfgPath, sqlDir, filepath.Join(dir, "output"),
				"api:\n  enabled: true\n  graphql:\n    enabled: true\n    schema_dir: ./graph\n    resolver_dir: ./graph\n    package: graph\n"+
					"tables:\n  owners:\n    relationships:\n      - "+tt.rel)
			writeSQLFile(t, sqlDir, schema)

			_, stderr, err := executeCommand("validate", "--config", cfgPath)
			var ee *exitError
			if !errors.As(err, &ee) || ee.code != ExitConfig {
				t.Fatalf("validate error = %v, want exit code %d (config)", err, ExitConfig)
			}
			const want = "Error: tables.owners.relationships[0].name: required"
			if got := strings.Count(stderr, "Error: "); got != 1 || !strings.Contains(stderr, want) {
				t.Errorf("stderr = %q, want the one error %q", stderr, want)
			}
		})
	}
}
