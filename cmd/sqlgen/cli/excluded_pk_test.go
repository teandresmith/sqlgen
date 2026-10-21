package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// TestExcludedPrimaryKeyColumn checks that `validate` and `generate` both
// reject `exclude_columns` removing a primary-key member with an error naming
// the config key and the column, instead of failing in a template or, for a
// composite key, generating on the surviving member alone.
func TestExcludedPrimaryKeyColumn(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		yaml   string
		want   string
	}{
		{
			name:   "only key column",
			schema: `CREATE TABLE cycles (id INT PRIMARY KEY, name TEXT NOT NULL);`,
			yaml:   "tables:\n  cycles:\n    exclude_columns: [id]\n",
			want:   `tables.public.cycles.exclude_columns removes primary-key column "id" of table public.cycles`,
		},
		{
			name:   "global exclusion",
			schema: `CREATE TABLE cycles (id INT PRIMARY KEY, name TEXT NOT NULL);`,
			yaml:   "generation:\n  exclude_columns: [id]\n",
			want:   `generation.exclude_columns removes primary-key column "id" of table public.cycles`,
		},
		{
			name: "one member of a composite key",
			schema: `CREATE TABLE memberships (org_id INT NOT NULL, user_id INT NOT NULL, role TEXT NOT NULL,
				PRIMARY KEY (org_id, user_id));`,
			yaml: "tables:\n  memberships:\n    exclude_columns: [user_id]\n",
			want: `tables.public.memberships.exclude_columns removes primary-key column "user_id" of table public.memberships`,
		},
	}
	for _, tt := range tests {
		for _, command := range []string{"validate", "generate"} {
			t.Run(tt.name+"/"+command, func(t *testing.T) {
				cfgPath := writeSchemaProject(t, config.DialectPostgres, tt.schema)
				appendConfig(t, cfgPath, tt.yaml)

				_, stderr, err := executeCommand(command, "--config", cfgPath)
				if err == nil {
					t.Fatalf("%s: want an error, got none\nstderr: %s", command, stderr)
				}
				if !strings.Contains(stderr, tt.want) {
					t.Errorf("%s stderr = %q, want it to contain %q", command, stderr, tt.want)
				}
				if strings.Contains(stderr, "executing template") {
					t.Errorf("%s stderr = %q, want no template failure", command, stderr)
				}
			})
		}
	}
}

// TestGenerate_AllColumnsExcludedSkipsTable checks PRD §4.13's exhaustion
// row: a table whose every column is excluded is skipped with a warning, and
// the rest of the schema generates.
func TestGenerate_AllColumnsExcludedSkipsTable(t *testing.T) {
	cfgPath := writeSchemaProject(t, config.DialectPostgres, `
		CREATE TABLE cycles (id INT PRIMARY KEY, name TEXT NOT NULL);
		CREATE TABLE tasks (id INT PRIMARY KEY, title TEXT NOT NULL);
	`)
	appendConfig(t, cfgPath, "tables:\n  cycles:\n    exclude_columns: [id, name]\n")

	_, stderr, err := executeCommand("generate", "--config", cfgPath)
	if err != nil {
		t.Fatalf("generate: unexpected error: %v\nstderr: %s", err, stderr)
	}
	const want = "Warning: exclude_columns removes all columns from table public.cycles; table will be skipped"
	if !strings.Contains(stderr, want) {
		t.Errorf("generate stderr = %q, want it to contain %q", stderr, want)
	}
	out := filepath.Join(filepath.Dir(cfgPath), "output")
	if _, err := os.Stat(filepath.Join(out, "task_gen.go")); err != nil {
		t.Errorf("task_gen.go: %v, want it generated", err)
	}
	if _, err := os.Stat(filepath.Join(out, "cycle_gen.go")); !os.IsNotExist(err) {
		t.Errorf("cycle_gen.go: stat error = %v, want the file absent", err)
	}
}
