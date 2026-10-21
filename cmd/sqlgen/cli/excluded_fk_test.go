package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

const excludedFKSchema = `
	CREATE TABLE cycles (id INT PRIMARY KEY, name TEXT NOT NULL);
	CREATE TABLE tasks (id INT PRIMARY KEY, title TEXT NOT NULL, cycle_id INT REFERENCES cycles(id));
`

// TestGenerate_ExcludedFKColumnOmitsEdge generates a schema whose only FK
// column is removed by `exclude_columns`. The O2M edge it would infer is
// omitted with a warning, so Cycle gains no Tasks field whose loader reads a
// CycleID that Task does not have.
func TestGenerate_ExcludedFKColumnOmitsEdge(t *testing.T) {
	cfgPath := writeSchemaProject(t, config.DialectPostgres, excludedFKSchema)
	appendConfig(t, cfgPath, "tables:\n  tasks:\n    exclude_columns: [cycle_id]\n")

	_, stderr, err := executeCommand("generate", "--config", cfgPath)
	if err != nil {
		t.Fatalf("generate: unexpected error: %v\nstderr: %s", err, stderr)
	}
	const want = "Warning: tables.public.cycles: the foreign-key-inferred relationship \"tasks\" is omitted because its key column public.tasks.cycle_id is removed by `exclude_columns`."
	if !strings.Contains(stderr, want) {
		t.Errorf("generate stderr = %q, want it to contain %q", stderr, want)
	}
	files := parseGeneratedFiles(t, filepath.Join(filepath.Dir(cfgPath), "output"))
	if diff := cmp.Diff([]string{"ID", "Name"}, structFieldNames(files, "Cycle")); diff != "" {
		t.Errorf("Cycle fields mismatch (-want +got):\n%s", diff)
	}
}

// TestValidate_NestedMutationOnExcludedFKEdge pins that `validate` sees the
// omitted edge too: a nested_mutations entry naming it names no relationship
// (PRD §4.8).
func TestValidate_NestedMutationOnExcludedFKEdge(t *testing.T) {
	cfgPath := writeSchemaProject(t, config.DialectPostgres, excludedFKSchema)
	appendConfig(t, cfgPath, "generation:\n  nested_mutations:\n    enabled: true\n"+
		"tables:\n  tasks:\n    exclude_columns: [cycle_id]\n"+
		"  cycles:\n    nested_mutations:\n      relationships:\n        - name: Tasks\n")

	_, stderr, err := executeCommand("validate", "--config", cfgPath)
	if err == nil {
		t.Fatalf("validate: want an error, got none\nstderr: %s", stderr)
	}
	const want = `tables.public.cycles.nested_mutations.relationships: "Tasks" names no relationship on this table (PRD §4.8)`
	if !strings.Contains(stderr, want) {
		t.Errorf("validate stderr = %q, want it to contain %q", stderr, want)
	}
}
