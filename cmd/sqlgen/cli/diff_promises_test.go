package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
)

// tflAppend appends text to the file at path.
func tflAppend(t *testing.T, path, text string) {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // test-local temp tree
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	writeFileOrFail(t, path, string(data)+text)
}

// TestDiff_ListingAndSummaryFormat pins the listing format for a run that
// creates, modifies and deletes: each changed file is one line
// `  <create|modify|delete>  <path>`, and the listing ends with
// `<N> files would change (<c> created, <m> modified, <d> deleted)`.
func TestDiff_ListingAndSummaryFormat(t *testing.T) {
	cfgPath := setupDiffProject(t, diffProjectOptions{noGraphQL: true})
	generateForDiff(t, cfgPath)

	models := filepath.Join("internal", "database")
	// create: a file generate writes is missing.
	if err := os.Remove(filepath.Join(models, "post_gen.go")); err != nil {
		t.Fatalf("removing post_gen.go: %v", err)
	}
	// modify: a file generate writes differs.
	tflAppend(t, filepath.Join(models, "user_gen.go"), "// edited\n")
	// delete: an orphan carrying sqlgen's header, which the stale sweep removes.
	writeFileOrFail(t, filepath.Join(models, "legacy_table_gen.go"), gen.Header(Version)+"\n\npackage database\n")

	stdout, code := runDiff(t, cfgPath)

	if code != ExitGeneration {
		t.Errorf("diff exit code = %d, want 1", code)
	}
	want := "  create  " + filepath.Join(models, "post_gen.go") + "\n" +
		"  modify  " + filepath.Join(models, "user_gen.go") + "\n" +
		"  delete  " + filepath.Join(models, "legacy_table_gen.go") + "\n" +
		"\n" +
		"3 files would change (1 created, 1 modified, 1 deleted)\n"
	if stdout != want {
		t.Errorf("diff stdout =\n%q\nwant\n%q", stdout, want)
	}
}

// TestDiff_CoversGraphQLResolverDirFiles pins that, with
// api.graphql.enabled: true, the scalar, translator and resolver files under
// resolver_dir are compared: editing one makes diff report `modify` for it
// and exit 1.
func TestDiff_CoversGraphQLResolverDirFiles(t *testing.T) {
	tests := []struct {
		name string
		file string // relative to the project root
	}{
		{name: "scalar file", file: filepath.Join("internal", "graph", "scalars_gen.go")},
		{name: "translator file", file: filepath.Join("internal", "graph", "input_translate_gen.go")},
		{name: "resolver file", file: filepath.Join("internal", "graph", "sqlgenresolver", "resolvers_gen.go")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The real column needs a float32 marshaler pair, which is what
			// makes generate write the scalar file.
			cfgPath := setupDiffProject(t, diffProjectOptions{schema: "CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL, score real NOT NULL);"})
			generateForDiff(t, cfgPath)
			if _, err := os.Stat(tt.file); err != nil {
				t.Fatalf("generate did not write %s: %v", tt.file, err)
			}
			if stdout, code := runDiff(t, cfgPath); code != ExitSuccess {
				t.Fatalf("diff on an up-to-date tree exit code = %d, want 0; stdout:\n%s", code, stdout)
			}

			tflAppend(t, tt.file, "// edited\n")
			stdout, code := runDiff(t, cfgPath)

			if code != ExitGeneration {
				t.Errorf("diff exit code = %d, want 1; stdout:\n%s", code, stdout)
			}
			if !strings.HasPrefix(stdout, "  modify  "+tt.file+"\n\n") {
				t.Errorf("diff stdout = %q, want the single line %q", stdout, "  modify  "+tt.file)
			}
		})
	}
}

// TestDiff_ManifestArtifactsNotCompared pins that the manifest stage is
// excluded: its JSON and markdown artifacts are not compared, so editing them
// on disk is not reported as a change.
func TestDiff_ManifestArtifactsNotCompared(t *testing.T) {
	cfgPath := setupDiffProject(t, diffProjectOptions{manifest: true, noGraphQL: true})
	if _, stderr, err := executeCommand("generate", "--manifest-timestamp", "2020-01-01T00:00:00Z", "--config", cfgPath); err != nil {
		t.Fatalf("generate failed: %v\nstderr: %s", err, stderr)
	}

	manifestDir := filepath.Join("internal", "database", "manifest")
	for _, name := range []string{"manifest_gen.json", "_index.md", "user.md"} {
		path := filepath.Join(manifestDir, name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("generate did not write manifest artifact %s: %v", path, err)
		}
		tflAppend(t, path, "\nedited\n")
	}

	stdout, code := runDiff(t, cfgPath)

	if code != ExitSuccess {
		t.Errorf("diff exit code = %d, want 0; stdout:\n%s", code, stdout)
	}
	if stdout != "no changes detected\n" {
		t.Errorf("diff stdout = %q, want %q", stdout, "no changes detected\n")
	}
}

// TestDiff_OrphanedAPISchemaPreviewsNothing pins that, with an orphaned API
// schema file, diff reports the error instead of previewing changes — not
// even the deletion the dropped table would otherwise cause.
func TestDiff_OrphanedAPISchemaPreviewsNothing(t *testing.T) {
	cfgPath := setupDiffProject(t, diffProjectOptions{})
	generateForDiff(t, cfgPath)

	// Dropping posts leaves post_gen.go to delete and post_gen.graphqls orphaned.
	writeFileOrFail(t, filepath.Join("migrations", "001_init.sql"), "CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);")
	// And a modification, so a preview would have a second kind to list.
	tflAppend(t, filepath.Join("internal", "database", "user_gen.go"), "// edited\n")

	stdout, stderr, err := executeCommand("diff", "--config", cfgPath)

	if code := exitCodeOf(t, err); code != ExitGeneration {
		t.Errorf("diff exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "holds schema files for entities no longer on the API (post_gen.graphqls)") {
		t.Errorf("diff stderr = %q, want the orphaned schema error", stderr)
	}
	if stdout != "" {
		t.Errorf("diff previewed changes alongside the orphan error; stdout:\n%s", stdout)
	}
}

// TestDiff_ManifestStageFailureOutsidePreview pins that a failure of the
// manifest stage, which diff does not run, is outside the preview: generate
// fails on it, diff reports the tree up to date.
func TestDiff_ManifestStageFailureOutsidePreview(t *testing.T) {
	tests := []struct {
		name      string
		breakIt   func(t *testing.T)
		genStderr string
	}{
		{
			name: "manifest artifacts cannot be written",
			breakIt: func(t *testing.T) {
				dir := filepath.Join("internal", "database", "manifest")
				if err := os.RemoveAll(dir); err != nil {
					t.Fatalf("removing %s: %v", dir, err)
				}
				writeFileOrFail(t, dir, "not a directory\n")
			},
			genStderr: "Manifest error: ",
		},
		{
			name: "invalid project MCP config refused",
			breakIt: func(t *testing.T) {
				writeFileOrFail(t, ".mcp.json", "{not json\n")
			},
			genStderr: "refusing to overwrite invalid JSON",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgPath := setupDiffProject(t, diffProjectOptions{manifest: true, noGraphQL: true})
			generateForDiff(t, cfgPath)
			tt.breakIt(t)

			_, genStderr, genErr := executeCommand("generate", "--config", cfgPath)
			if code := exitCodeOf(t, genErr); code != ExitGeneration {
				t.Fatalf("generate exit code = %d, want 1; the fixture must make the manifest stage fail; stderr:\n%s", code, genStderr)
			}
			if !strings.Contains(genStderr, tt.genStderr) {
				t.Errorf("generate stderr = %q, want it to contain %q", genStderr, tt.genStderr)
			}

			stdout, code := runDiff(t, cfgPath)
			if code != ExitSuccess || stdout != "no changes detected\n" {
				t.Errorf("diff exit code = %d, stdout = %q; want 0 and %q", code, stdout, "no changes detected\n")
			}
		})
	}
}
