package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// tscUnusedScalarProject writes a module root holding a postgres schema, a
// consumer gqlgen.yml and a sqlgen.yml whose api.graphql.scalars declares
// EmailAddress for a go_type no column resolves to, then chdirs into it. The
// gqlgen invocation is a stub that exits 0 without writing anything, so the
// wrapper path runs to completion without a real gqlgen. Returns the config
// path.
func tscUnusedScalarProject(t *testing.T) string {
	t.Helper()
	stub, err := exec.LookPath("true")
	if err != nil {
		t.Skipf("no `true` binary to stand in for gqlgen: %v", err)
	}
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":                "module example.com/shop\n\ngo 1.26\n",
		"migrations/schema.sql": "CREATE TABLE contacts (id uuid PRIMARY KEY, name text NOT NULL);\n",
		"gqlgen.yml":            "schema:\n  - models/graph/*.graphqls\n",
		"sqlgen.yml":            tscUnusedScalarConfig(stub),
	}
	for rel, body := range files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("creating %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("writing %s: %v", rel, err)
		}
	}
	t.Chdir(dir)
	return filepath.Join(dir, "sqlgen.yml")
}

func tscUnusedScalarConfig(gqlgenBin string) string {
	return `version: v1
input:
  dialect: postgres
  paths:
    - ./migrations
output:
  driver: pgx
  dir: ./models
  package: models
api:
  enabled: true
  graphql:
    enabled: true
    schema_dir: ./models/graph
    resolver_dir: ./models/graph
    package: graph
    gqlgen_config: ./gqlgen.yml
    gqlgen_bin: ` + gqlgenBin + `
    scalars:
      EmailAddress:
        go_type: example.com/types.Email
        marshaling: method
`
}

// tscUnusedScalarWarning is the fragment every unresolved-declaration warning
// for the fixture's EmailAddress entry carries.
const tscUnusedScalarWarning = "api.graphql.scalars.EmailAddress: no API-exposed column resolves to"

// TestUnusedScalarWarning_firesFromGenerateAndValidateOnly pins PRD §26.4.1
// "Unresolved declarations warn": the check "fires from `generate` and
// `validate`, not from `graphql gen`". A chained `generate` — which runs the
// wrapper inline — still reports the entry exactly once.
func TestUnusedScalarWarning_firesFromGenerateAndValidateOnly(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		seed      bool // run a plain generate first: graphql gen reads the schema files it writes
		wantCount int
	}{
		{name: "generate warns", args: []string{"generate", "--no-graphql"}, wantCount: 1},
		{name: "generate chained with graphql gen warns once", args: []string{"generate"}, wantCount: 1},
		{name: "validate warns", args: []string{"validate"}, wantCount: 1},
		{name: "graphql gen does not warn", args: []string{"graphql", "gen"}, seed: true, wantCount: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgPath := tscUnusedScalarProject(t)
			args := append(append([]string{}, tt.args...), "--config", cfgPath)
			if tt.seed {
				if _, stderr, err := executeCommand("generate", "--no-graphql", "--config", cfgPath); err != nil {
					t.Fatalf("seeding generate: %v\nstderr: %s", err, stderr)
				}
			}
			stdout, stderr, err := executeCommand(args...)
			if err != nil {
				t.Fatalf("sqlgen %s: %v\nstdout: %s\nstderr: %s", strings.Join(tt.args, " "), err, stdout, stderr)
			}
			got := strings.Count(stdout+stderr, tscUnusedScalarWarning)
			if got != tt.wantCount {
				t.Errorf("sqlgen %s reported the unused EmailAddress declaration %d times, want %d\nstdout: %s\nstderr: %s",
					strings.Join(tt.args, " "), got, tt.wantCount, stdout, stderr)
			}
		})
	}
}
