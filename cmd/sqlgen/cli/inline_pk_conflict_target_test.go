package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGeneratePipeline_InlinePKRestatedInConfigKeepsConflictTarget drives the
// real `generate` command: a column-level PRIMARY KEY is a declared constraint
// with an index behind it, so restating that same key in
// tables.<name>.primary_key.columns must not drop the PK conflict target.
func TestGeneratePipeline_InlinePKRestatedInConfigKeepsConflictTarget(t *testing.T) {
	tests := []struct {
		name      string
		dialect   string
		driver    string
		schema    string
		tableYAML string
		want      bool
	}{
		{
			name:      "postgres app-enforced key emits no PK target",
			dialect:   "postgres",
			schema:    "CREATE TABLE products (\n    id UUID NOT NULL,\n    name TEXT NOT NULL\n);",
			tableYAML: "  products:\n    primary_key:\n      columns: [id]\n",
			want:      false,
		},
		{
			name:      "postgres inline PK restated in config",
			want:      true,
			dialect:   "postgres",
			schema:    "CREATE TABLE products (\n    id UUID PRIMARY KEY,\n    name TEXT NOT NULL\n);",
			tableYAML: "  products:\n    primary_key:\n      columns: [id]\n",
		},
		{
			name:      "mysql inline PK restated in config",
			want:      true,
			dialect:   "mysql",
			driver:    "stdlib",
			schema:    "CREATE TABLE products (\n    id BIGINT PRIMARY KEY,\n    name TEXT NOT NULL\n);",
			tableYAML: "  products:\n    primary_key:\n      columns: [id]\n",
		},
		{
			name:      "sqlite inline PK restated in config",
			want:      true,
			dialect:   "sqlite",
			driver:    "stdlib",
			schema:    "CREATE TABLE products (\n    id TEXT PRIMARY KEY,\n    name TEXT NOT NULL\n);",
			tableYAML: "  products:\n    primary_key:\n      columns: [id]\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			models := generateInlinePKModels(t, tt.dialect, tt.driver, tt.schema, tt.tableYAML)

			const target = "ProductConflictPK ProductConflictTarget = iota // PRIMARY KEY (id)"
			if got := strings.Contains(models, target); got != tt.want {
				t.Errorf("generated models contain %q = %v, want %v", target, got, tt.want)
			}
		})
	}
}

// generateInlinePKModels runs `generate` for a one-file schema with the given
// `tables:` body and returns the generated models_gen.go.
func generateInlinePKModels(t *testing.T, dialect, driver, schema, tablesYAML string) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "schema.sql"), []byte(schema), 0o600); err != nil {
		t.Fatalf("writing schema: %v", err)
	}

	driverYAML := ""
	if driver != "" {
		driverYAML = "  driver: " + driver + "\n"
	}
	cfg := "version: 1\ninput:\n  dialect: " + dialect + "\n  paths: [schema.sql]\noutput:\n" +
		driverYAML + "  dir: out\n  package: models\ntables:\n" + tablesYAML
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	t.Chdir(dir)

	if _, stderr, err := executeCommand("generate", "--config", cfgPath); err != nil {
		t.Fatalf("generate returned error: %v\nstderr: %s", err, stderr)
	}

	models, err := os.ReadFile(filepath.Join(dir, "out", "models_gen.go")) //nolint:gosec // test code, path is from controlled test dir
	if err != nil {
		t.Fatalf("reading generated models: %v", err)
	}
	return string(models)
}
