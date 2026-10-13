package main_test

import (
	"bytes"
	"errors"
	"flag"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/cli"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/internal/livedb"
)

var updateE2E = flag.Bool("update-e2e", false, "update E2E golden files in expected/ directories")

const examplesDir = "testdata/examples"

// goldenManifestTimestamp pins the manifest's generated_at field so
// manifest_gen.json / breadcrumbs are byte-deterministic across golden runs
// (PRD §30.3 Determinism). Without the flag the manifest stage falls back to
// time.Now().UTC(), which would churn the goldens on every run. Every generate
// invocation in this harness threads this value through --manifest-timestamp;
// examples that leave manifest disabled ignore it (the flag is inert).
const goldenManifestTimestamp = "2026-01-01T00:00:00Z"

// exampleDir represents a discovered E2E example project.
type exampleDir struct {
	// Name is the directory name (e.g., "postgres").
	Name string
	// Path is the absolute path to the example directory.
	Path string
}

// discoverExamples finds all subdirectories of examplesDir that contain a sqlgen.yml file.
func discoverExamples(t *testing.T) []exampleDir {
	t.Helper()

	entries, err := os.ReadDir(examplesDir)
	if err != nil {
		t.Fatalf("reading examples dir %s: %v", examplesDir, err)
	}

	var examples []exampleDir
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dir := filepath.Join(examplesDir, entry.Name())
		cfgPath := filepath.Join(dir, "sqlgen.yml")
		if _, err := os.Stat(cfgPath); err != nil {
			continue // no sqlgen.yml, skip
		}

		absDir, err := filepath.Abs(dir)
		if err != nil {
			t.Fatalf("resolving absolute path for %s: %v", dir, err)
		}

		examples = append(examples, exampleDir{
			Name: entry.Name(),
			Path: absDir,
		})
	}

	return examples
}

// TestE2EDiscovery verifies that the harness discovers example directories correctly.
func TestE2EDiscovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	examples := discoverExamples(t)

	// Verify only directories with sqlgen.yml are discovered.
	for _, ex := range examples {
		cfgPath := filepath.Join(ex.Path, "sqlgen.yml")
		if _, err := os.Stat(cfgPath); err != nil {
			t.Errorf("discovered example %q has no sqlgen.yml", ex.Name)
		}
	}
}

// TestE2EGoldenFiles runs the generate-and-compare cycle for each example.
func TestE2EGoldenFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	examples := discoverExamples(t)
	if len(examples) == 0 {
		t.Skip("no example directories found")
	}

	for _, ex := range examples {
		t.Run(ex.Name, func(t *testing.T) {
			runGoldenFileTest(t, ex)
		})
	}
}

// runGoldenFileTest generates code for an example and compares against expected/ golden files.
func runGoldenFileTest(t *testing.T, ex exampleDir) {
	t.Helper()

	// Generation uses relative paths from the config file, so we need to
	// run from the example directory. Use a temp dir for output to avoid
	// polluting the repo, unless we're updating golden files.
	cfgPath := filepath.Join(ex.Path, "sqlgen.yml")

	// Save and restore the working directory — generation resolves relative paths.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getting working dir: %v", err)
	}
	if err := os.Chdir(ex.Path); err != nil {
		t.Fatalf("changing to example dir %s: %v", ex.Path, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(origDir); err != nil {
			t.Fatalf("restoring working dir: %v", err)
		}
	})

	// GOWORK=off is required so the gqlgen subprocess (`go run …`) can use
	// `GOFLAGS=-mod=mod` without colliding with the repo's go.work.
	// Set it for every example — the existing non-graphql examples don't
	// invoke a `go run` subprocess, so the override is a safe no-op for them.
	t.Setenv("GOWORK", "off")

	cfgData, err := os.ReadFile(cfgPath) //nolint:gosec // test helper, path is controlled
	if err != nil {
		t.Fatalf("reading config %s: %v", cfgPath, err)
	}

	if inputValue(cfgData, "source") == "both" {
		runBothGoldenFileTest(t, ex, cfgPath, cfgData)
		return
	}

	// Graphql examples need a real Go-module context for the chained
	// `sqlgen graphql gen` gqlgen subprocess — gqlgen infers import paths
	// from go.mod, so a bare temp output dir resolves to the empty import
	// path and collides the exec/model packages. Update mode generates in
	// place at ex.Path and copies the tree into expected/. Compare mode
	// generates in a copy of the whole example (generateCopy), so a failing
	// run never rewrites the checked-in models/.
	//
	// Root-relative schema_dir/resolver_dir (§26.5.8) can place the graph
	// package as a sibling of output.dir (e.g. a top-level ./graph), outside
	// the output.dir walk. siblingGraphDirs folds any such tree into the
	// golden set under its root-relative path; a graph nested under
	// output.dir (e.g. models/graph) is already walked.
	if isGraphQLExample(ex.Path, cfgData) {
		if *updateE2E {
			runGenerate(t, cfgPath)
			copyOutputToExpected(t, ex.Path, siblingGraphDirs(t, ex.Path, cfgData, findOutputDir(t, ex.Path)))
			t.Logf("updated golden files for %s", ex.Name)
			return
		}
		compareFilesWithExpected(t, ex.Path, generateCopy(t, ex, cfgData, cfgData))
		return
	}

	if *updateE2E {
		// Generate in-place and update the expected/ directory.
		runGenerate(t, cfgPath)
		copyOutputToExpected(t, ex.Path, nil)
		t.Logf("updated golden files for %s", ex.Name)
		return
	}

	// Generate into a temp directory and compare against expected/.
	tmpOutput := t.TempDir()
	runGenerateToDir(t, ex.Path, cfgPath, tmpOutput)
	compareWithExpected(t, ex.Path, tmpOutput)
}

// runBothGoldenFileTest generates a source: both example from its base
// database (setup only), with migrations/ as the overlay, and compares the
// output with expected/ byte for byte. The same tree must then come from
// source: database on the migrated database and from source: files over
// schema.sql plus migrations/, less the livedb.ParityExceptions (PRD §6.1).
// The working directory is the example's.
func runBothGoldenFileTest(t *testing.T, ex exampleDir, cfgPath string, cfgData []byte) {
	t.Helper()
	dialect := inputValue(cfgData, "dialect")
	t.Setenv("SQLGEN_DB_URL", "") // it would override input.connection.url
	base := newExampleDatabase(t, dialect, ex.Name)
	livedb.ApplySetup(t, dialect, base, ex.Path)
	t.Setenv("SQLGEN_EXAMPLE_DATABASE_URL", base)

	if *updateE2E {
		runGenerate(t, cfgPath)
		copyOutputToExpected(t, ex.Path, nil)
		t.Logf("updated golden files for %s", ex.Name)
		return
	}
	tmpOutput := t.TempDir()
	runGenerateToDir(t, ex.Path, cfgPath, tmpOutput)
	compareWithExpected(t, ex.Path, tmpOutput)

	migrated := newExampleDatabase(t, dialect, "")
	livedb.ApplySetup(t, dialect, migrated, ex.Path)
	livedb.ApplyMigrations(t, dialect, migrated, ex.Path)
	t.Run("database", func(t *testing.T) {
		compareBySource(t, dialect, "database", ex, generateCopy(t, ex, cfgData, rewriteToDatabaseSource(cfgData, migrated)))
	})
	t.Run("files", func(t *testing.T) {
		compareBySource(t, dialect, "files", ex, generateCopy(t, ex, cfgData, rewriteToFilesSource(cfgData)))
	})
}

// newExampleDatabase creates an empty database for dialect and returns its
// connection string. A non-empty name fixes the MySQL database's name, which
// MySQL writes into each view's stored definition and so into the manifest.
func newExampleDatabase(t *testing.T, dialect, name string) string {
	t.Helper()
	switch dialect {
	case "postgres":
		return livedb.NewPostgresDatabase(t)
	case "mysql":
		if name != "" {
			return livedb.NewNamedMySQLDatabase(t, name)
		}
		return livedb.NewMySQLDatabase(t)
	case "sqlite":
		return livedb.NewSQLiteDatabase(t)
	}
	t.Fatalf("unknown input.dialect %q", dialect)
	return ""
}

// runGenerate runs sqlgen generate using the config's output.dir (in-place).
func runGenerate(t *testing.T, cfgPath string) {
	t.Helper()

	rootCmd := cli.NewRootCmd()
	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"generate", "--config", cfgPath, "--manifest-timestamp", goldenManifestTimestamp})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("sqlgen generate failed:\nstdout: %s\nstderr: %s\nerror: %v",
			stdout.String(), stderr.String(), err)
	}
}

// runGenerateToDir runs sqlgen generate, overriding output to a temp directory.
// It does this by copying the config, modifying the output.dir, and running generation.
//
// Rewriting output.dir also redirects API-side import paths to the
// temp dir, which leaks absolute /var/folders/... paths into generated Go
// files and breaks the golden diff. Set the SQLGEN_OUTPUT_DIR_IMPORT_OVERRIDE
// env var to the originally configured output.dir so import paths stay stable
// while writes still go to the temp directory.
//
// Note: this path is not used for graphql examples — those generate in a
// copy of the whole example (generateCopy) because the chained `sqlgen
// graphql gen` gqlgen subprocess infers import paths from the example's
// go.mod, and a bare temp output dir has no go.mod so gqlgen collides on
// the empty import path.
func runGenerateToDir(t *testing.T, _ /*examplePath*/, cfgPath, outputDir string) {
	t.Helper()

	// Read the original config and rewrite output.dir to our temp directory.
	cfgData, err := os.ReadFile(cfgPath) //nolint:gosec // test helper, path is controlled
	if err != nil {
		t.Fatalf("reading config %s: %v", cfgPath, err)
	}

	originalOutputDir := extractOutputDir(t, cfgData)
	tmpCfg := rewriteOutputDir(t, cfgData, outputDir)
	// Write the temp config to a separate directory so it doesn't pollute
	// the generated output directory (which is diffed against expected/).
	cfgDir := t.TempDir()
	tmpCfgPath := filepath.Join(cfgDir, "sqlgen.yml")

	if err := os.WriteFile(tmpCfgPath, tmpCfg, 0o600); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}

	if originalOutputDir != "" {
		t.Setenv("SQLGEN_OUTPUT_DIR_IMPORT_OVERRIDE", originalOutputDir)
	}

	rootCmd := cli.NewRootCmd()
	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"generate", "--config", tmpCfgPath, "--manifest-timestamp", goldenManifestTimestamp})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("sqlgen generate failed:\nstdout: %s\nstderr: %s\nerror: %v",
			stdout.String(), stderr.String(), err)
	}
}

// isGraphQLExample reports whether the example has api.graphql.enabled in
// sqlgen.yml AND a gqlgen.yml at the example root. Both must be true: the
// chained `sqlgen graphql gen` requires gqlgen.yml on disk, and
// the harness only does extra path rewiring when that combo is present.
func isGraphQLExample(examplePath string, cfgData []byte) bool {
	if _, err := os.Stat(filepath.Join(examplePath, "gqlgen.yml")); err != nil {
		return false
	}
	return apiGraphQLEnabled(cfgData)
}

// apiGraphQLEnabled reports whether the YAML enables api.graphql. Uses a
// simple line-based parser (mirrors extractOutputDir) so the test file
// stays free of a YAML library dependency.
func apiGraphQLEnabled(cfgData []byte) bool {
	lines := strings.Split(string(cfgData), "\n")
	apiEnabled, graphqlEnabled, inAPI, inGraphQL := false, false, false, false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Detect when we leave the api section (non-indented line that isn't empty
		// and isn't the api: header).
		if inAPI && len(trimmed) > 0 && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && !strings.HasPrefix(trimmed, "api:") {
			inAPI = false
			inGraphQL = false
		}

		if strings.HasPrefix(trimmed, "api:") {
			inAPI = true
			inGraphQL = false
			continue
		}

		if !inAPI {
			continue
		}

		// Within the api: block, top-level keys (api.enabled / api.graphql:)
		// are at the same indent depth as `enabled:` directly under api:.
		// We rely on the convention that graphql: has no inline value to
		// detect entering the graphql sub-mapping.
		if strings.HasPrefix(trimmed, "graphql:") {
			inGraphQL = true
			continue
		}
		if strings.HasPrefix(trimmed, "rest:") || strings.HasPrefix(trimmed, "grpc:") {
			inGraphQL = false
			continue
		}

		if rest, ok := strings.CutPrefix(trimmed, "enabled:"); ok {
			value := strings.TrimSpace(rest)
			if inGraphQL {
				graphqlEnabled = (value == "true")
			} else {
				apiEnabled = (value == "true")
			}
		}
	}

	return apiEnabled && graphqlEnabled
}

// siblingGraphDirs returns the root-relative graph output dirs (api.graphql
// schema_dir / resolver_dir) that resolve OUTSIDE output.dir, mapped to their
// absolute on-disk location. Under root-relative resolution (§26.5.8) a
// top-level schema_dir like `graph` places the graph package as a sibling of
// output.dir, which the output.dir-only golden diff would miss. Graph dirs
// nested under output.dir (e.g. models/graph) are already covered by the
// output.dir walk and are excluded here (the map is empty for that layout).
func siblingGraphDirs(t *testing.T, examplePath string, cfgData []byte, outputDirAbs string) map[string]string {
	t.Helper()

	out := make(map[string]string)
	for _, rel := range extractGraphDirs(cfgData) {
		graphAbs := filepath.Join(examplePath, rel)
		relToOutput, err := filepath.Rel(outputDirAbs, graphAbs)
		// Nested under (or equal to) output.dir when the relative path does not
		// escape upward — already walked by the output.dir diff, so skip.
		if err == nil && relToOutput != ".." && !strings.HasPrefix(relToOutput, ".."+string(filepath.Separator)) {
			continue
		}
		out[rel] = graphAbs
	}
	return out
}

// extractGraphDirs returns the cleaned root-relative graph output dirs declared
// under api.graphql (schema_dir + resolver_dir), deduplicated. Line-based to
// avoid a YAML dependency in the test harness (mirrors apiGraphQLEnabled).
func extractGraphDirs(cfgData []byte) []string {
	lines := strings.Split(string(cfgData), "\n")
	inAPI, inGraphQL := false, false
	seen := make(map[string]bool)
	var dirs []string

	add := func(v string) {
		v = filepath.Clean(strings.TrimSpace(v))
		if v == "" || v == "." || seen[v] {
			return
		}
		seen[v] = true
		dirs = append(dirs, v)
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if inAPI && len(trimmed) > 0 && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && !strings.HasPrefix(trimmed, "api:") {
			inAPI = false
			inGraphQL = false
		}
		if strings.HasPrefix(trimmed, "api:") {
			inAPI = true
			inGraphQL = false
			continue
		}
		if !inAPI {
			continue
		}
		if strings.HasPrefix(trimmed, "graphql:") {
			inGraphQL = true
			continue
		}
		if strings.HasPrefix(trimmed, "rest:") || strings.HasPrefix(trimmed, "grpc:") {
			inGraphQL = false
			continue
		}
		if !inGraphQL {
			continue
		}
		if rest, ok := strings.CutPrefix(trimmed, "schema_dir:"); ok {
			add(rest)
		}
		if rest, ok := strings.CutPrefix(trimmed, "resolver_dir:"); ok {
			add(rest)
		}
	}
	return dirs
}

// extractOutputDir reads the output.dir value from a YAML config blob using
// the same line-based parser as rewriteOutputDir. Returns empty when the key
// is absent.
func extractOutputDir(t *testing.T, cfgData []byte) string {
	t.Helper()

	lines := strings.Split(string(cfgData), "\n")
	inOutput := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "output:") {
			inOutput = true
			continue
		}

		if inOutput && len(trimmed) > 0 && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			inOutput = false
		}

		if inOutput && strings.HasPrefix(trimmed, "dir:") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "dir:"))
		}
	}

	return ""
}

// rewriteOutputDir replaces the output.dir value in a YAML config with the given directory.
// This is a simple line-based replacement to avoid importing a YAML library in tests.
func rewriteOutputDir(t *testing.T, cfgData []byte, newDir string) []byte {
	t.Helper()

	lines := strings.Split(string(cfgData), "\n")
	var result []string
	inOutput := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "output:") {
			inOutput = true
			result = append(result, line)
			continue
		}

		// Detect when we leave the output section (non-indented line that isn't empty).
		if inOutput && len(trimmed) > 0 && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			inOutput = false
		}

		if inOutput && strings.HasPrefix(trimmed, "dir:") {
			// Determine the indentation of the current line.
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			result = append(result, indent+"dir: "+newDir)
			continue
		}

		result = append(result, line)
	}

	return []byte(strings.Join(result, "\n"))
}

// copyOutputToExpected copies the generated output directory into expected/.
// It reads output.dir from the config, then mirrors that into expected/. Any
// sibling graph dirs (root-relative graph trees outside output.dir) are
// mirrored under expected/<root-relative-path> so the golden set matches the
// on-disk layout.
func copyOutputToExpected(t *testing.T, examplePath string, siblings map[string]string) {
	t.Helper()

	outputDir := findOutputDir(t, examplePath)
	expectedDir := filepath.Join(examplePath, "expected")

	// Remove old expected files.
	if err := os.RemoveAll(expectedDir); err != nil {
		t.Fatalf("removing old expected dir: %v", err)
	}

	// Copy output to expected.
	if err := copyDir(outputDir, expectedDir); err != nil {
		t.Fatalf("copying output to expected: %v", err)
	}

	// Copy each sibling graph tree into expected/<root-relative-path>. Done
	// after the output copy because that wipes expected/ first.
	for rel, abs := range siblings {
		if err := copyDir(abs, filepath.Join(expectedDir, rel)); err != nil {
			t.Fatalf("copying sibling graph dir %q to expected: %v", rel, err)
		}
	}
}

// compareWithExpected diffs the output dir tmpOutputDir against the expected/
// golden directory.
func compareWithExpected(t *testing.T, examplePath, tmpOutputDir string) {
	t.Helper()
	compareFilesWithExpected(t, examplePath, slashKeys(collectFiles(t, tmpOutputDir)))
}

// compareFilesWithExpected diffs generatedFiles, keyed by slash path, against
// the expected/ golden directory.
func compareFilesWithExpected(t *testing.T, examplePath string, generatedFiles map[string]string) {
	t.Helper()

	expectedDir := filepath.Join(examplePath, "expected")
	if _, err := os.Stat(expectedDir); err != nil {
		t.Fatalf("expected/ directory does not exist in %s (run with -update-e2e to create)", examplePath)
	}
	expectedFiles := slashKeys(collectFiles(t, expectedDir))

	// Check for files in expected/ but missing from generated output.
	for relPath := range expectedFiles {
		if _, ok := generatedFiles[relPath]; !ok {
			t.Errorf("file %q exists in expected/ but was not generated", relPath)
		}
	}

	// Check for files generated but not in expected/.
	for relPath := range generatedFiles {
		if _, ok := expectedFiles[relPath]; !ok {
			t.Errorf("file %q was generated but does not exist in expected/", relPath)
		}
	}

	// Diff matching files.
	for relPath, expectedContent := range expectedFiles {
		generatedContent, ok := generatedFiles[relPath]
		if !ok {
			continue // already reported above
		}

		if diff := cmp.Diff(expectedContent, generatedContent); diff != "" {
			t.Errorf("file %q differs from expected (-want +got):\n%s", relPath, diff)
		}
	}
}

// collectFiles walks a directory and returns a map of relative path -> file content.
// `*_test.go` files are skipped because sqlgen never emits test files — those
// are hand-written next to the generated package (e.g. `mysql_version_test.go`)
// and live in the output dir for package-internal access. Including them in
// the golden diff would force every hand-written test to be regenerated, which
// is the opposite of the intent.
func collectFiles(t *testing.T, root string) map[string]string {
	t.Helper()

	files := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		content, err := os.ReadFile(path) //nolint:gosec // test helper, walking controlled directory
		if err != nil {
			return err
		}

		files[relPath] = string(content)
		return nil
	})
	if err != nil {
		t.Fatalf("walking directory %s: %v", root, err)
	}

	return files
}

// findOutputDir reads the sqlgen.yml config and extracts the output.dir value.
func findOutputDir(t *testing.T, examplePath string) string {
	t.Helper()

	cfgPath := filepath.Join(examplePath, "sqlgen.yml")
	data, err := os.ReadFile(cfgPath) //nolint:gosec // test helper
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}

	// Simple line-based extraction of output.dir.
	lines := strings.Split(string(data), "\n")
	inOutput := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "output:") {
			inOutput = true
			continue
		}

		if inOutput && len(trimmed) > 0 && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			inOutput = false
		}

		if inOutput && strings.HasPrefix(trimmed, "dir:") {
			dir := strings.TrimSpace(strings.TrimPrefix(trimmed, "dir:"))
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(examplePath, dir)
			}
			return dir
		}
	}

	t.Fatalf("output.dir not found in %s", cfgPath)
	return ""
}

// copyDir recursively copies a directory tree.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if d.IsDir() {
			return os.MkdirAll(dstPath, 0o750)
		}

		data, err := os.ReadFile(path) //nolint:gosec // copying within test fixture
		if err != nil {
			return err
		}

		if err := os.MkdirAll(filepath.Dir(dstPath), 0o750); err != nil {
			return err
		}

		return os.WriteFile(dstPath, data, 0o600) //nolint:gosec // copying within test fixture
	})
}

// TestE2EDatabaseTwin generates every source: files example from a database
// built from the example's own setup files, and compares the output with
// expected/ less the livedb.ParityExceptions (PRD §6.1). It generates into a
// copy of the example and never writes goldens, so -update-e2e does not apply.
func TestE2EDatabaseTwin(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	examples := discoverExamples(t)
	if len(examples) == 0 {
		t.Fatal("no example directories found")
	}
	for _, ex := range examples {
		cfgData, err := os.ReadFile(filepath.Join(ex.Path, "sqlgen.yml")) //nolint:gosec // test reads an example's own config
		if err != nil {
			t.Fatal(err)
		}
		if src := inputValue(cfgData, "source"); src != "" && src != "files" {
			continue
		}
		t.Run(ex.Name, func(t *testing.T) {
			runDatabaseTwin(t, ex, cfgData)
		})
	}
}

// runDatabaseTwin builds ex's database, generates a copy of ex under
// source: database, and compares the copy's output with ex's expected/.
func runDatabaseTwin(t *testing.T, ex exampleDir, cfgData []byte) {
	t.Helper()
	dialect := inputValue(cfgData, "dialect")
	conn := newExampleDatabase(t, dialect, "")
	livedb.ApplySetup(t, dialect, conn, ex.Path)
	compareBySource(t, dialect, "database", ex, generateCopy(t, ex, cfgData, rewriteToDatabaseSource(cfgData, conn)))
}

// generateCopy generates a copy of ex whose sqlgen.yml is cfg, and returns
// the output, keyed by slash path as expected/ lays it out. cfgData is ex's
// own config. The copy keeps a real module context for the chained gqlgen
// subprocess, and the checked-in models/ stays untouched.
func generateCopy(t *testing.T, ex exampleDir, cfgData, cfg []byte) map[string]string {
	t.Helper()
	dir := t.TempDir()
	if err := copyDir(ex.Path, dir); err != nil {
		t.Fatalf("copying example: %v", err)
	}
	absoluteReplaces(t, ex.Path, filepath.Join(dir, "go.mod"))
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	if err := os.WriteFile(cfgPath, cfg, 0o600); err != nil { //nolint:gosec // test writes its own temp copy
		t.Fatal(err)
	}

	outputDir := findOutputDir(t, dir)
	siblings := siblingGraphDirs(t, dir, cfgData, outputDir)
	removeGoldenOutputs(t, filepath.Join(ex.Path, "expected"), outputDir, siblings)

	t.Chdir(dir)
	t.Setenv("GOWORK", "off")
	// SQLGEN_DB_URL would override input.connection.url.
	t.Setenv("SQLGEN_DB_URL", "")
	runGenerate(t, cfgPath)

	generated := collectFiles(t, outputDir)
	for rel, abs := range siblings {
		for k, v := range collectFiles(t, abs) {
			generated[filepath.Join(rel, k)] = v
		}
	}
	return slashKeys(generated)
}

// compareBySource compares a tree generated under source with ex's
// expected/, after livedb.NormalizeTree on both.
func compareBySource(t *testing.T, dialect, source string, ex exampleDir, generated map[string]string) {
	t.Helper()
	want := livedb.NormalizeTree(t, dialect, slashKeys(collectFiles(t, filepath.Join(ex.Path, "expected"))))
	got := livedb.NormalizeTree(t, dialect, generated)
	for _, path := range slices.Sorted(maps.Keys(want)) {
		content, ok := got[path]
		if !ok {
			t.Errorf("%s is in expected/ but source: %s did not generate it", path, source)
			continue
		}
		if diff := cmp.Diff(want[path], content); diff != "" {
			t.Errorf("%s differs by source (-expected +%s):\n%s", path, source, diff)
		}
	}
	for _, path := range slices.Sorted(maps.Keys(got)) {
		if _, ok := want[path]; !ok {
			t.Errorf("%s was generated under source: %s but is not in expected/", path, source)
		}
	}
}

// removeGoldenOutputs deletes from a copy of an example every file its
// expected/ lists, so a file the run fails to emit cannot pass as the
// checked-in one. gqlgen keeps resolver implementations in resolver.go
// and *.resolvers.go, so those stay.
func removeGoldenOutputs(t *testing.T, expectedDir, outputDir string, siblings map[string]string) {
	t.Helper()
	for rel := range collectFiles(t, expectedDir) {
		name := filepath.Base(rel)
		if name == "resolver.go" || strings.HasSuffix(name, ".resolvers.go") {
			continue
		}
		path := filepath.Join(outputDir, rel)
		for sibling, abs := range siblings {
			if inner, ok := strings.CutPrefix(rel, sibling+string(filepath.Separator)); ok {
				path = filepath.Join(abs, inner)
			}
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Fatal(err)
		}
	}
}

// absoluteReplaces rewrites each relative replace target in the go.mod at
// path against examplePath, so the copied module still finds the repo.
func absoluteReplaces(t *testing.T, examplePath, path string) {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // test reads its own copy
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		before, target, ok := strings.Cut(line, "=>")
		if target = strings.TrimSpace(target); !ok || !strings.HasPrefix(target, ".") {
			continue
		}
		lines[i] = before + "=> " + filepath.ToSlash(filepath.Join(examplePath, target))
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600); err != nil { //nolint:gosec // test writes its own temp copy
		t.Fatal(err)
	}
}

// inputValue returns the scalar value of key directly under the top-level
// input: block, or "" when it is absent. Line-based, as extractOutputDir is.
func inputValue(cfgData []byte, key string) string {
	inInput := false
	for line := range strings.Lines(string(cfgData)) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			inInput = strings.HasPrefix(trimmed, "input:")
			continue
		}
		if rest, ok := strings.CutPrefix(trimmed, key+":"); ok && inInput && strings.HasPrefix(line, "  ") && line[2] != ' ' {
			value, _, _ := strings.Cut(rest, "#")
			return strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	return ""
}

// rewriteToDatabaseSource returns cfgData with input.source: database and
// input.connection.url set to conn, and input.paths removed.
func rewriteToDatabaseSource(cfgData []byte, conn string) []byte {
	return rewriteInput(cfgData, []string{"source", "connection", "paths"},
		"  source: database", "  connection:", "    url: "+strconv.Quote(conn))
}

// rewriteToFilesSource returns the config of a source: both example under
// source: files: input.source and input.connection removed, and schema.sql
// read ahead of the input.paths migrations.
func rewriteToFilesSource(cfgData []byte) []byte {
	rewritten := rewriteInput(cfgData, []string{"source", "connection"})
	return []byte(strings.Replace(string(rewritten), "\n  paths:\n", "\n  paths:\n    - ./schema.sql\n", 1))
}

// rewriteInput returns cfgData with each input: key in drop removed, value
// included, and the lines add inserted at the top of input:. It assumes the
// two-space indent every example uses.
func rewriteInput(cfgData []byte, drop []string, add ...string) []byte {
	var out []string
	inInput, skipping := false, false
	for line := range strings.Lines(string(cfgData)) {
		line = strings.TrimSuffix(line, "\n")
		trimmed := strings.TrimSpace(line)
		indented := strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")
		if trimmed != "" && !indented && !strings.HasPrefix(trimmed, "#") {
			inInput = strings.HasPrefix(trimmed, "input:")
			skipping = false
			out = append(out, line)
			if inInput {
				out = append(out, add...)
			}
			continue
		}
		if inInput && trimmed != "" {
			// A removed key's value lines are indented deeper, or are list
			// items at the key's own indent.
			if skipping && (strings.HasPrefix(line, "   ") || strings.HasPrefix(trimmed, "- ")) {
				continue
			}
			key, _, _ := strings.Cut(trimmed, ":")
			skipping = slices.Contains(drop, key)
			if skipping {
				continue
			}
		}
		out = append(out, line)
	}
	return []byte(strings.Join(out, "\n") + "\n")
}

// slashKeys returns files keyed by slash path, the form NormalizeTree takes.
func slashKeys(files map[string]string) map[string]string {
	out := make(map[string]string, len(files))
	for k, v := range files {
		out[filepath.ToSlash(k)] = v
	}
	return out
}

// TestRewriteToDatabaseSource loads every example's twin config through the
// real config loader: it must differ from the original only in input.source,
// input.connection and input.paths, so a twin cannot quietly run under files.
func TestRewriteToDatabaseSource(t *testing.T) {
	const conn = "postgres://localhost:5432/db?sslmode=disable"
	for _, ex := range discoverExamples(t) {
		t.Run(ex.Name, func(t *testing.T) {
			testRewrite(t, ex, func(cfgData []byte) []byte { return rewriteToDatabaseSource(cfgData, conn) },
				func(want *config.RootConfig) {
					want.Input.Source = config.SourceDatabase
					want.Input.Connection = &config.ConnectionConfig{URL: conn}
					want.Input.Paths = []string{"."}
				})
		})
	}
}

// TestRewriteToFilesSource loads every source: both example's files config
// through the real config loader: it must differ from the original only in
// input.source, input.connection and schema.sql ahead of input.paths.
func TestRewriteToFilesSource(t *testing.T) {
	var checked int
	for _, ex := range discoverExamples(t) {
		cfgData, err := os.ReadFile(filepath.Join(ex.Path, "sqlgen.yml")) //nolint:gosec // test reads an example's own config
		if err != nil {
			t.Fatal(err)
		}
		if inputValue(cfgData, "source") != "both" {
			continue
		}
		checked++
		t.Run(ex.Name, func(t *testing.T) {
			testRewrite(t, ex, rewriteToFilesSource, func(want *config.RootConfig) {
				want.Input.Source = config.SourceFiles
				want.Input.Connection = nil
				want.Input.Paths = append([]string{"./schema.sql"}, want.Input.Paths...)
			})
		})
	}
	if checked == 0 {
		t.Fatal("no source: both example found")
	}
}

// testRewrite checks that rewrite turns ex's config into the one edit makes
// of it, comparing both as config.LoadConfig loads them.
func testRewrite(t *testing.T, ex exampleDir, rewrite func([]byte) []byte, edit func(*config.RootConfig)) {
	t.Helper()
	// The source: both examples read their connection from this variable.
	t.Setenv("SQLGEN_EXAMPLE_DATABASE_URL", "postgres://localhost:5432/base?sslmode=disable")
	ignoreUnexported := cmp.FilterPath(func(p cmp.Path) bool {
		f, ok := p.Last().(cmp.StructField)
		return ok && !token.IsExported(f.Name())
	}, cmp.Ignore())

	cfgPath := filepath.Join(ex.Path, "sqlgen.yml")
	cfgData, err := os.ReadFile(cfgPath) //nolint:gosec // test reads an example's own config
	if err != nil {
		t.Fatal(err)
	}
	want, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if dialect := inputValue(cfgData, "dialect"); dialect != string(want.Input.Dialect) {
		t.Errorf("inputValue(dialect) = %q, want %q", dialect, want.Input.Dialect)
	}
	edit(want)

	rewritten := filepath.Join(t.TempDir(), "sqlgen.yml")
	if err := os.WriteFile(rewritten, rewrite(cfgData), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := config.LoadConfig(rewritten)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got, ignoreUnexported); diff != "" {
		t.Errorf("rewritten config (-want +got):\n%s", diff)
	}
}
