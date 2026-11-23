package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// A detected edge whose Go field is a column's is omitted: validate and
// generate both warn, the column keeps the field, and the package builds
// (PRD §4.13).
func TestGenerate_DetectedEdgeOnAColumnFieldWarns(t *testing.T) {
	const schema = `CREATE TABLE users (id bigint PRIMARY KEY, posts text);
CREATE TABLE posts (id bigint PRIMARY KEY, user_id bigint REFERENCES users(id));`
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		dialect, driver, table string
	}{
		{"postgres", "pgx", "public.users"},
		{"mysql", "stdlib", "users"},
		{"sqlite", "stdlib", "users"},
	}
	for _, tt := range tests {
		t.Run(tt.dialect, func(t *testing.T) {
			cfg := "version: 1\ninput:\n  dialect: " + tt.dialect + "\n  paths: [schema.sql]\n" +
				"output:\n  driver: " + tt.driver + "\n  dir: out\n  package: models\n"
			want := "Warning: tables." + tt.table + `: the foreign-key-inferred relationship "posts" is omitted because its Go field "Posts" is the field of column "posts". ` +
				"Declare the edge in `relationships` under another name to keep it, or list \"posts\" in `exclude_relationships` to silence this (PRD §4.13, §13.4).\n"
			for _, command := range []string{"validate", "generate"} {
				dir, stderr, err := runCommand(t, command, schema, cfg)
				if err != nil {
					t.Fatalf("%s: %v\nstderr: %s", command, err, stderr)
				}
				if strings.Count(stderr, want) != 1 {
					t.Errorf("%s stderr = %q, want the collision warning once", command, stderr)
				}
				if command != "generate" {
					continue
				}
				files := parseGeneratedFiles(t, filepath.Join(dir, "out"))
				if diff := cmp.Diff([]string{"ID", "Posts"}, structFieldNames(files, "User")); diff != "" {
					t.Errorf("User fields mismatch (-want +got):\n%s", diff)
				}
				if testing.Short() && os.Getenv("SQLGEN_BUILD_GENERATED") == "" {
					continue
				}
				buildGenerated(t, root, dir)
			}
		})
	}
}
