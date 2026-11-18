package gen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
)

// TestGenerate_TenancyTypeWithoutImportBuilds pins that a tenancy.type naming
// a builtin or a type declared in the generated package itself, with no
// import, generates a package that builds: no empty import is emitted, and the
// tenant column is spelled with the named type throughout.
//
// The config is handed to Generate without ValidatePreParse, so the test
// exercises the generator whatever pre-parse validation decides about an
// import-less tenancy.type.
func TestGenerate_TenancyTypeWithoutImportBuilds(t *testing.T) {
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	const ddl = `
CREATE TABLE projects (
  id bigint PRIMARY KEY,
  workspace_id bigint NOT NULL,
  name text NOT NULL
);`
	tests := []struct {
		name     string
		goType   string
		userFile string // a hand-written file in the generated package, when the type lives there
	}{
		{name: "builtin", goType: "int64"},
		{name: "same-package type", goType: "WorkspaceID", userFile: "package models\n\ntype WorkspaceID int64\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			cfgYAML := "version: v1\ninput:\n  dialect: postgres\n  paths: [./schema.sql]\noutput:\n  dir: ./models\n  package: models\n" +
				"tenancy:\n  enabled: true\n  column: workspace_id\n  type:\n    type: " + tt.goType + "\n"
			for name, body := range map[string]string{"schema.sql": ddl, "sqlgen.yml": cfgYAML} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
					t.Fatalf("writing %s: %v", name, err)
				}
			}
			t.Chdir(root)

			schema := parseDDLForTest(t, config.DialectPostgres, ddl)
			cfg, err := config.LoadConfig(filepath.Join(root, "sqlgen.yml"))
			if err != nil {
				t.Fatalf("LoadConfig() error: %v", err)
			}
			if _, err := gen.Generate(schema, cfg, "test"); err != nil {
				t.Fatalf("Generate() error: %v", err)
			}
			if tt.userFile != "" {
				if err := os.WriteFile(filepath.Join(root, "models", "workspace.go"), []byte(tt.userFile), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			models, err := os.ReadFile(filepath.Join(root, "models", "models_gen.go")) //nolint:gosec // test-written temp module
			if err != nil {
				t.Fatal(err)
			}
			if want := "WorkspaceID " + tt.goType; !opContainsField(string(models), want) {
				t.Errorf("models_gen.go does not spell the tenant column as %q", want)
			}

			vetGeneratedModels(t, root, repo)
		})
	}
}

// opContainsField reports whether src declares a struct field "<name> <type>",
// tolerating the column alignment gofmt adds between the two.
func opContainsField(src, field string) bool {
	name, typ, _ := strings.Cut(field, " ")
	for line := range strings.SplitSeq(src, "\n") {
		parts := strings.Fields(line)
		if len(parts) >= 2 && parts[0] == name && parts[1] == typ {
			return true
		}
	}
	return false
}
