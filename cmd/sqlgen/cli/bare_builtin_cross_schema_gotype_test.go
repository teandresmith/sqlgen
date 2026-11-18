package cli

import (
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestGenerate_BareBuiltinIgnoresOtherSchemaType generates a schema where only
// app declares a type named after a built-in (integer) and a public table has
// int columns written bare. PostgreSQL resolves a bare built-in name to the
// pg_catalog type in every schema but the one sqlgen reads it relative to, so
// the bare-name fallback to app's type must not answer for it: the columns take
// the built-in Go type. A bare name that is not a built-in still falls back to
// the only schema declaring it.
func TestGenerate_BareBuiltinIgnoresOtherSchemaType(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		want   map[string]string
	}{
		{
			name: "domain named after a built-in",
			schema: `CREATE SCHEMA app;
CREATE DOMAIN app.integer AS text;
CREATE TABLE t (id bigint PRIMARY KEY, a int, b int NOT NULL);
`,
			want: map[string]string{"ID": "int64", "A": "*int32", "B": "int32"},
		},
		{
			name: "enum named after a built-in",
			schema: `CREATE SCHEMA app;
CREATE TYPE app.integer AS ENUM ('one', 'two');
CREATE TABLE t (id bigint PRIMARY KEY, a int, b int NOT NULL);
`,
			want: map[string]string{"ID": "int64", "A": "*int32", "B": "int32"},
		},
		{
			name: "array of a domain named after a built-in",
			schema: `CREATE SCHEMA app;
CREATE DOMAIN app.integer AS text;
CREATE TABLE t (id bigint PRIMARY KEY, a int[]);
`,
			want: map[string]string{"ID": "int64", "A": "[]int32"},
		},
		{
			name: "domain with a name no built-in has",
			schema: `CREATE SCHEMA app;
CREATE DOMAIN app.score AS integer;
CREATE TABLE t (id bigint PRIMARY KEY, a score, b score NOT NULL);
`,
			want: map[string]string{"ID": "int64", "A": "*int32", "B": "int32"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "schema.sql", tt.schema)
			cfgPath := writeFile(t, dir, "sqlgen.yml", `version: v1
input:
  dialect: postgres
  paths:
    - ./schema.sql
output:
  driver: pgx
  dir: ./models
  package: models
`)
			t.Chdir(dir)
			if _, stderr, err := executeCommand("generate", "--config", cfgPath); err != nil {
				t.Fatalf("generate: %v\nstderr: %s", err, stderr)
			}

			got := gtwStructFieldTypes(t, filepath.Join(dir, "models", "models_gen.go"), "T")
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("struct T field types mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
