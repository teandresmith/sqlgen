package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestGenerate_UnrecognizedViewDirectiveWarns pins that an annotation
// directive the view parser does not know is ignored with a warning naming
// it and its file, under generate and validate alike, and that the command
// still succeeds.
func TestGenerate_UnrecognizedViewDirectiveWarns(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "generate"},
		{name: "validate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stderr := runWithUnrecognizedDirective(t, tt.name)
			const want = "views/user_names.sql: unrecognized annotation directive @primary ignored"
			var warned bool
			for line := range strings.SplitSeq(stderr, "\n") {
				if strings.HasPrefix(line, "Warning:") && strings.HasSuffix(line, want) {
					warned = true
				}
			}
			if !warned {
				t.Errorf("stderr = %q, want a Warning line ending %q", stderr, want)
			}
		})
	}
}

func runWithUnrecognizedDirective(t *testing.T, command string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"schema.sql":           "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);\n",
		"views/user_names.sql": "-- @pk: id\n-- @primary: id\n\nCREATE VIEW user_names AS SELECT id, name FROM users;\n",
		"sqlgen.yml": `version: 1
input:
  dialect: sqlite
  paths: [schema.sql]
  views: [views/]
output:
  driver: stdlib
  dir: out
  package: models
`,
	}
	for name, content := range files {
		writeFile(t, filepath.Join(dir, filepath.Dir(name)), filepath.Base(name), content)
	}
	t.Chdir(dir)
	_, stderr, err := executeCommand(command, "--config", filepath.Join(dir, "sqlgen.yml"))
	if err != nil {
		t.Fatalf("%s: %v\nstderr: %s", command, err, stderr)
	}
	return stderr
}
