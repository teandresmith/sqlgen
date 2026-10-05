package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
)

// The `sqlgen diff` contract is that it writes nothing (PRD §23.6). Redirecting
// output.dir does not deliver that on its own: api.graphql.schema_dir /
// resolver_dir are root-relative (§26.5.8) and resolve independently of
// output.dir, so a GraphQL-enabled project used to have its whole graph package
// rewritten — with the throwaway dir baked into the models import path — by a
// command that reported "no changes detected". Every test below is built around
// a GraphQL-enabled project for that reason.

const diffTestSchema = `CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);
CREATE TABLE posts (id serial PRIMARY KEY, title text NOT NULL);`

// setupDiffProject writes a GraphQL-enabled project into a temp dir, chdirs
// into it, and returns the config path. The topology mirrors the shape that
// surfaced the bug: a models package and a graph package as separate top-level
// siblings, neither reachable from the other's directory.
//
// A real go.mod is written because the resolver-side emission is gated on a
// resolvable module path — without one the sqlgenresolver sub-package, the
// half that carried the corrupted import, never gets generated at all.
func setupDiffProject(t *testing.T, opts diffProjectOptions) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)

	writeFileOrFail(t, filepath.Join(dir, "go.mod"), "module example.com/proj\n\ngo 1.25\n")

	if err := os.MkdirAll(filepath.Join(dir, "migrations"), 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	schema := opts.schema
	if schema == "" {
		schema = diffTestSchema
	}
	writeFileOrFail(t, filepath.Join(dir, "migrations", "001_init.sql"), schema)

	cfg := `version: v1
input:
  dialect: postgres
  paths:
    - ./migrations
output:
  driver: pgx
  dir: ./internal/database
  package: database
  layout: ` + orDefault(opts.layout, "file_per_table") + `
`
	if opts.manifest {
		cfg += `generation:
  manifest:
    enabled: true
`
	}
	if !opts.noGraphQL {
		cfg += `api:
  enabled: true
  graphql:
    enabled: true
    schema_dir: ./internal/graph
    resolver_dir: ./internal/graph
    package: graph
`
	}

	cfgPath := filepath.Join(dir, "sqlgen.yml")
	writeFileOrFail(t, cfgPath, cfg)
	return cfgPath
}

type diffProjectOptions struct {
	schema    string
	layout    string
	manifest  bool
	noGraphQL bool
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func writeFileOrFail(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// snapshotTree hashes every file under root, keyed by its path relative to
// root, so a test can assert that a command left the working tree untouched
// down to the byte. Content rather than mtime: a rewrite with identical bytes
// is still a write into a tree the command promised not to touch, and this
// catches it via the permission bits when nothing else changes.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path) //nolint:gosec // test-local temp tree
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		out[rel] = hex.EncodeToString(sum[:]) + " " + info.Mode().String()
		return nil
	})
	if err != nil {
		t.Fatalf("snapshotting %s: %v", root, err)
	}
	return out
}

// generateInto runs a real generation so the tree starts out up to date.
// --no-graphql suppresses the chained gqlgen wrapper, which shells out to the
// network; the sqlgen-owned half of the graph package is emitted either way,
// and that is the half this file is about.
func generateForDiff(t *testing.T, cfgPath string) {
	t.Helper()
	if _, stderr, err := executeCommand("generate", "--no-graphql", "--config", cfgPath); err != nil {
		t.Fatalf("generate failed: %v\nstderr: %s", err, stderr)
	}
}

// runDiff executes the diff command and returns its stdout and exit code.
func runDiff(t *testing.T, cfgPath string) (string, int) {
	t.Helper()
	stdout, stderr, err := executeCommand("diff", "--config", cfgPath)
	if err == nil {
		return stdout, 0
	}
	var ee *exitError
	if !errors.As(err, &ee) {
		t.Fatalf("diff returned a non-exit error: %v\nstderr: %s", err, stderr)
	}
	return stdout, ee.code
}

// TestDiff_upToDateGraphQLProject_writesNothing is the regression test for the
// whole defect. Before the fix this reported "no changes detected" while
// rewriting thirteen files under the graph package, five of them with a models
// import pointing at the deleted temp dir — leaving a tree that had compiled a
// moment earlier unable to build.
func TestDiff_upToDateGraphQLProject_writesNothing(t *testing.T) {
	cfgPath := setupDiffProject(t, diffProjectOptions{})
	generateForDiff(t, cfgPath)

	root := filepath.Dir(cfgPath)
	before := snapshotTree(t, root)

	stdout, code := runDiff(t, cfgPath)

	if code != 0 {
		t.Errorf("diff exit code = %d, want 0 for an up-to-date tree; stdout:\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "no changes") {
		t.Errorf("diff stdout = %q, want it to report no changes", stdout)
	}
	if diff := cmp.Diff(before, snapshotTree(t, root)); diff != "" {
		t.Errorf("diff modified the working tree (-before +after):\n%s", diff)
	}
}

// TestDiff_graphPackageUntouched pins the specific files the old redirect
// missed, so a regression names the graph package rather than showing up as an
// opaque tree diff.
func TestDiff_graphPackageUntouched(t *testing.T) {
	cfgPath := setupDiffProject(t, diffProjectOptions{})
	generateForDiff(t, cfgPath)

	root := filepath.Dir(cfgPath)
	graphDir := filepath.Join(root, "internal", "graph")
	before := snapshotTree(t, graphDir)
	if len(before) == 0 {
		t.Fatalf("no graph files generated at %s; the fixture is not exercising the GraphQL path", graphDir)
	}

	if _, code := runDiff(t, cfgPath); code != 0 {
		t.Errorf("diff exit code = %d, want 0", code)
	}

	if diff := cmp.Diff(before, snapshotTree(t, graphDir)); diff != "" {
		t.Errorf("diff rewrote the graph package (-before +after):\n%s", diff)
	}
}

// TestDiff_staleSchema_reportsGraphDriftAndWritesNothing covers the case the
// command exists for: the tree really is out of date. The graph files have to
// show up in the report — leaving them out under-reports drift on exactly the
// projects whose graph package the old code corrupted — and still must not be
// written.
func TestDiff_staleSchema_reportsGraphDriftAndWritesNothing(t *testing.T) {
	cfgPath := setupDiffProject(t, diffProjectOptions{})
	generateForDiff(t, cfgPath)

	root := filepath.Dir(cfgPath)
	writeFileOrFail(t, filepath.Join(root, "migrations", "001_init.sql"),
		diffTestSchema+"\nCREATE TABLE widgets (id serial PRIMARY KEY, label text NOT NULL);")

	before := snapshotTree(t, root)
	stdout, code := runDiff(t, cfgPath)

	if code != 1 {
		t.Errorf("diff exit code = %d, want 1 when changes are detected; stdout:\n%s", code, stdout)
	}
	for _, want := range []string{
		filepath.Join("internal", "graph", "widget_gen.graphqls"),
		filepath.Join("internal", "database", "widget_gen.go"),
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("diff stdout does not mention %s; got:\n%s", want, stdout)
		}
	}
	if diff := cmp.Diff(before, snapshotTree(t, root)); diff != "" {
		t.Errorf("diff modified the working tree (-before +after):\n%s", diff)
	}
}

// TestDiff_freshProject_createsNothing covers the first-run case, where the
// old code silently materialized a half-generated graph package in a tree the
// user had generated nothing into yet.
func TestDiff_freshProject_createsNothing(t *testing.T) {
	cfgPath := setupDiffProject(t, diffProjectOptions{})
	root := filepath.Dir(cfgPath)
	before := snapshotTree(t, root)

	stdout, code := runDiff(t, cfgPath)

	if code != 1 {
		t.Errorf("diff exit code = %d, want 1 when everything would be created; stdout:\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "create") {
		t.Errorf("diff stdout = %q, want it to report creations", stdout)
	}
	if strings.Contains(stdout, "modify") || strings.Contains(stdout, "delete") {
		t.Errorf("diff on an ungenerated tree reported something other than creations:\n%s", stdout)
	}
	if diff := cmp.Diff(before, snapshotTree(t, root)); diff != "" {
		t.Errorf("diff created files in the working tree (-before +after):\n%s", diff)
	}
}

// TestDiff_manifestEmbedNotReportedAsDeleted covers the false positive: the
// manifest stage writes manifest_embed_gen.go after gen.Generate returns, so a
// preview run's written set omits it and the stale sweep used to report a
// deletion that `sqlgen generate` never performs.
func TestDiff_manifestEmbedNotReportedAsDeleted(t *testing.T) {
	cfgPath := setupDiffProject(t, diffProjectOptions{manifest: true})
	generateForDiff(t, cfgPath)

	embed := filepath.Join(filepath.Dir(cfgPath), "internal", "database", gen.ManifestEmbedFilename)
	if _, err := os.Stat(embed); err != nil {
		t.Fatalf("generate did not write %s: %v; the fixture is not exercising the manifest stage", embed, err)
	}

	stdout, code := runDiff(t, cfgPath)

	if strings.Contains(stdout, gen.ManifestEmbedFilename) {
		t.Errorf("diff reported %s, which generate regenerates rather than deletes; stdout:\n%s",
			gen.ManifestEmbedFilename, stdout)
	}
	if code != 0 {
		t.Errorf("diff exit code = %d, want 0 for an up-to-date tree; stdout:\n%s", code, stdout)
	}
}

// TestDiff_reportsGenuineDeletion holds the other side of that line: a
// *_gen.go file whose table is gone really is stale, and diff must say so
// without acting on it.
func TestDiff_reportsGenuineDeletion(t *testing.T) {
	cfgPath := setupDiffProject(t, diffProjectOptions{})
	generateForDiff(t, cfgPath)

	root := filepath.Dir(cfgPath)
	orphan := filepath.Join(root, "internal", "database", "post_gen.go")
	if _, err := os.Stat(orphan); err != nil {
		t.Fatalf("generate did not write %s: %v", orphan, err)
	}

	writeFileOrFail(t, filepath.Join(root, "migrations", "001_init.sql"),
		"CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);")
	// The dropped table's API schema goes too, as generate's own error
	// instructs; left in place it fails the run before any sweep
	// (TestDiff_orphanedAPISchema_failsLikeGenerate).
	if err := os.Remove(filepath.Join(root, "internal", "graph", "post_gen.graphqls")); err != nil {
		t.Fatalf("removing the dropped table's schema file: %v", err)
	}

	stdout, code := runDiff(t, cfgPath)

	if code != 1 {
		t.Errorf("diff exit code = %d, want 1; stdout:\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "delete") || !strings.Contains(stdout, "post_gen.go") {
		t.Errorf("diff did not report post_gen.go as a deletion; stdout:\n%s", stdout)
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Errorf("diff deleted %s; it must only report the deletion: %v", orphan, err)
	}
}

// TestDiff_orphanedAPISchema_failsLikeGenerate pins PRD §23.8. A
// dropped table leaves its `*_gen.graphqls` in schema_dir, where gqlgen would
// load it; generate refuses to run, names the file and the gqlgen resolver file
// beside it, and deletes neither. It refuses before writing anything: a refusal
// after the models package was regenerated left the package uncompilable, and
// reverting the config did not recover it without another run. diff must refuse
// the same way. It generates into a temp dir, so a check that looked there
// instead of at the working tree would let it preview a deletion of post_gen.go
// that generate never reaches.
func TestDiff_orphanedAPISchema_failsLikeGenerate(t *testing.T) {
	cfgPath := setupDiffProject(t, diffProjectOptions{})
	generateForDiff(t, cfgPath)

	root := filepath.Dir(cfgPath)
	schemaOrphan := filepath.Join(root, "internal", "graph", "post_gen.graphqls")
	// generateForDiff skips gqlgen, so stand in for the resolver file its
	// follow-schema layout would have written.
	resolverOrphan := filepath.Join(root, "internal", "graph", "post_gen.resolvers.go")
	writeFileOrFail(t, resolverOrphan, "package graph\n")
	writeFileOrFail(t, filepath.Join(root, "migrations", "001_init.sql"),
		"CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);")

	before := snapshotTree(t, root)
	_, diffStderr, diffErr := executeCommand("diff", "--config", cfgPath)
	if diff := cmp.Diff(before, snapshotTree(t, root)); diff != "" {
		t.Errorf("diff modified the working tree (-before +after):\n%s", diff)
	}
	_, genStderr, genErr := executeCommand("generate", "--no-graphql", "--config", cfgPath)
	if diff := cmp.Diff(before, snapshotTree(t, root)); diff != "" {
		t.Errorf("refused generate modified the working tree (-before +after):\n%s", diff)
	}

	const want = "Generation error: api: internal/graph holds schema files for entities no longer on the API (post_gen.graphqls), " +
		"which gqlgen would load and fail on; delete them, and the gqlgen resolver files that go with them " +
		"(internal/graph/post_gen.resolvers.go) after moving out any resolvers written by hand (PRD §23.8)"
	for _, run := range []struct {
		cmd    string
		stderr string
		err    error
	}{{"diff", diffStderr, diffErr}, {"generate", genStderr, genErr}} {
		if ee, ok := errors.AsType[*exitError](run.err); !ok || ee.code != ExitGeneration {
			t.Errorf("%s error = %v, want exit code %d", run.cmd, run.err, ExitGeneration)
		}
		if !strings.Contains(run.stderr, want) {
			t.Errorf("%s stderr = %q, want it to contain %q", run.cmd, run.stderr, want)
		}
	}
	for _, p := range []string{schemaOrphan, resolverOrphan} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("generate removed %s; it must only report it: %v", p, err)
		}
	}

	// Deleting what the error names is the whole recovery.
	for _, p := range []string{schemaOrphan, resolverOrphan} {
		if err := os.Remove(p); err != nil {
			t.Fatalf("removing %s: %v", p, err)
		}
	}
	generateForDiff(t, cfgPath)
	if stdout, code := runDiff(t, cfgPath); code != 0 {
		t.Errorf("diff after recovery exit code = %d, want 0; stdout:\n%s", code, stdout)
	}
}

// TestDiff_nonGraphQLProject_writesNothing guards the path that was already
// correct, so the sandbox rework cannot regress it.
func TestDiff_nonGraphQLProject_writesNothing(t *testing.T) {
	cfgPath := setupDiffProject(t, diffProjectOptions{noGraphQL: true})
	generateForDiff(t, cfgPath)

	root := filepath.Dir(cfgPath)
	before := snapshotTree(t, root)

	stdout, code := runDiff(t, cfgPath)

	if code != 0 {
		t.Errorf("diff exit code = %d, want 0; stdout:\n%s", code, stdout)
	}
	if diff := cmp.Diff(before, snapshotTree(t, root)); diff != "" {
		t.Errorf("diff modified the working tree (-before +after):\n%s", diff)
	}
}

// TestGenerateInto_redirectsEveryWrite_andKeepsImportsConfigured is the
// generator-level half of the fix, stated directly rather than through diff's
// reporting.
//
// Two things have to hold at once, and the old code got both wrong: nothing
// may be written outside the redirect root, and nothing written may mention
// the root. The second is what broke consumers — the models import path was
// computed from the redirected output dir, so the emitted graph package
// imported a directory that was about to be deleted.
func TestGenerateInto_redirectsEveryWrite_andKeepsImportsConfigured(t *testing.T) {
	cfgPath := setupDiffProject(t, diffProjectOptions{})
	root := filepath.Dir(cfgPath)

	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	schema, err := parseSchema(cfg)
	if err != nil {
		t.Fatalf("parsing schema: %v", err)
	}
	applyPrimaryKeyOverrides(schema, cfg)

	before := snapshotTree(t, root)
	writeRoot := t.TempDir()

	result, err := gen.GenerateInto(schema, cfg, "test", writeRoot)
	if err != nil {
		t.Fatalf("GenerateInto: %v", err)
	}

	if diff := cmp.Diff(before, snapshotTree(t, root)); diff != "" {
		t.Errorf("GenerateInto wrote into the project tree (-before +after):\n%s", diff)
	}

	var sawGraph bool
	for _, f := range result.Files {
		if !strings.HasPrefix(f, writeRoot+string(filepath.Separator)) {
			t.Errorf("GenerateInto wrote %s, which is outside the redirect root %s", f, writeRoot)
			continue
		}
		dir, ok := result.WriteDirs[filepath.Dir(f)]
		if !ok {
			t.Errorf("WriteDirs has no entry for %s, so diff cannot pair it with the working tree", filepath.Dir(f))
			continue
		}
		if strings.Contains(dir, writeRoot) {
			t.Errorf("WriteDirs maps %s back to %s, which still names the redirect root", f, dir)
		}
		if strings.Contains(dir, filepath.Join("internal", "graph")) {
			sawGraph = true
		}

		data, err := os.ReadFile(f) //nolint:gosec // path comes from the generator's own written-file list
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		if strings.Contains(string(data), writeRoot) {
			t.Errorf("%s names the redirect root %s in its contents; a redirected run must emit what a real run emits",
				filepath.Base(f), writeRoot)
		}
	}

	if !sawGraph {
		t.Error("no file was written to the graph package; the fixture is not exercising the path that escaped the redirect")
	}
}
