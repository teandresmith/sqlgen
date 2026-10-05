package gen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheckAPISchemaOrphans pins the orphan rule (PRD §23.8): a
// `*_gen.graphqls` in the working tree's schema dir that this run will not
// write fails generation, naming the file and — when one exists — the gqlgen
// resolver file that goes with it. Nothing is ever deleted. That the check runs
// before anything is written, and that `sqlgen diff` agrees with `generate`, is
// pinned end to end by TestDiff_orphanedAPISchema_failsLikeGenerate.
//
// The test runs from a temp working dir so the dirs, and therefore the
// messages, are the relative paths a consumer sees.
func TestCheckAPISchemaOrphans(t *testing.T) {
	tests := []struct {
		name        string
		schemaDir   string
		resolverDir string
		// onDisk is created under the working dir before the check; a trailing
		// slash makes a directory.
		onDisk []string
		// names are the schema files the run will write (apiSchemaFileNames).
		names   []string
		wantErr string
	}{
		{
			name:   "files this run will write are not orphans",
			onDisk: []string{"graph/user_gen.graphqls", "graph/shared_gen.graphqls", "graph/user_gen.resolvers.go", "graph/resolver.go"},
			names:  []string{"user_gen.graphqls", "shared_gen.graphqls"},
		},
		{
			// A first run, or a new entity: nothing on disk yet for a name.
			name:   "a schema file still to be created is not missing",
			onDisk: []string{"graph/shared_gen.graphqls"},
			names:  []string{"user_gen.graphqls", "shared_gen.graphqls"},
		},
		{
			name:   "consumer schema files without the _gen suffix are left alone",
			onDisk: []string{"graph/schema.graphqls", "graph/custom.graphqls", "graph/shared_gen.graphqls"},
			names:  []string{"shared_gen.graphqls"},
		},
		{
			name:   "a directory is not a schema file",
			onDisk: []string{"graph/archive_gen.graphqls/", "graph/shared_gen.graphqls"},
			names:  []string{"shared_gen.graphqls"},
		},
		{
			name:  "a schema dir that does not exist yet has no orphans",
			names: []string{"shared_gen.graphqls"},
		},
		{
			name: "orphans under follow-schema layout name their resolver files",
			onDisk: []string{
				"graph/shared_gen.graphqls",
				"graph/event_gen.graphqls", "graph/event_gen.resolvers.go",
				"graph/numeric_width_gen.graphqls", "graph/numeric_width_gen.resolvers.go",
			},
			names: []string{"shared_gen.graphqls"},
			wantErr: "api: graph holds schema files for entities no longer on the API (event_gen.graphqls, numeric_width_gen.graphqls), " +
				"which gqlgen would load and fail on; delete them, and the gqlgen resolver files that go with them " +
				"(graph/event_gen.resolvers.go, graph/numeric_width_gen.resolvers.go) after moving out any resolvers written by hand (PRD §23.8)",
		},
		{
			// Single-file layout keeps every resolver in resolver.go, and gqlgen
			// moves the stale methods aside itself once the schema file is gone.
			name:   "an orphan with no resolver file names only the schema file",
			onDisk: []string{"graph/shared_gen.graphqls", "graph/numeric_width_gen.graphqls", "graph/resolver.go"},
			names:  []string{"shared_gen.graphqls"},
			wantErr: "api: graph holds schema files for entities no longer on the API (numeric_width_gen.graphqls), " +
				"which gqlgen would load and fail on; delete them (PRD §23.8)",
		},
		{
			name:        "the resolver file is looked up in the resolver dir",
			schemaDir:   "schema",
			resolverDir: "graph",
			onDisk:      []string{"schema/shared_gen.graphqls", "schema/post_gen.graphqls", "schema/post_gen.resolvers.go", "graph/post_gen.resolvers.go"},
			names:       []string{"shared_gen.graphqls"},
			wantErr: "api: schema holds schema files for entities no longer on the API (post_gen.graphqls), " +
				"which gqlgen would load and fail on; delete them, and the gqlgen resolver files that go with them " +
				"(graph/post_gen.resolvers.go) after moving out any resolvers written by hand (PRD §23.8)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			for _, p := range tt.onDisk {
				seedPath(t, p)
			}
			schemaDir, resolverDir := orDefaultDir(tt.schemaDir), orDefaultDir(tt.resolverDir)

			err := checkAPISchemaOrphans(schemaDir, resolverDir, tt.names)

			got := ""
			if err != nil {
				got = err.Error()
			}
			if got != tt.wantErr {
				t.Errorf("checkAPISchemaOrphans(%q, %q, %q) error = %q, want %q", schemaDir, resolverDir, tt.names, got, tt.wantErr)
			}
			for _, p := range tt.onDisk {
				if _, err := os.Stat(p); err != nil {
					t.Errorf("checkAPISchemaOrphans() removed %s; it must only report: %v", p, err)
				}
			}
		})
	}
}

func orDefaultDir(dir string) string {
	if dir == "" {
		return "graph"
	}
	return dir
}

// seedPath creates p relative to the working dir: a directory when p ends in
// a slash, otherwise an empty file.
func seedPath(t *testing.T, p string) {
	t.Helper()
	if strings.HasSuffix(p, "/") {
		if err := os.MkdirAll(p, 0o750); err != nil {
			t.Fatalf("creating %s: %v", p, err)
		}
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatalf("creating %s: %v", filepath.Dir(p), err)
	}
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatalf("seeding %s: %v", p, err)
	}
}
