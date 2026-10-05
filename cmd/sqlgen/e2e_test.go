package main_test

import (
	"bytes"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/cli"
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

	// Graphql examples need a real Go-module context for the chained
	// `sqlgen graphql gen` gqlgen subprocess — gqlgen infers
	// import paths from go.mod, and absolute paths under /tmp resolve to
	// the empty import path, colliding the exec/model packages. Rather
	// than synthesize a fake module on every run, generate in-place at
	// ex.Path and diff against expected/. Update mode copies the freshly
	// regenerated tree into expected/ to refresh the goldens.
	if isGraphQLExample(ex.Path, cfgData) {
		runGenerate(t, cfgPath)
		outputDir := findOutputDir(t, ex.Path)
		// Root-relative schema_dir/resolver_dir (§26.5.8) can place the graph
		// package as a sibling of output.dir (e.g. a top-level ./graph). Such a
		// tree lives outside output.dir, so the output.dir-only diff below would
		// silently skip it. Capture any graph dir that resolves outside
		// output.dir and fold it into the golden set under its root-relative
		// path. Graph nested under output.dir (e.g. models/graph) is already
		// covered by the output.dir walk, so siblings is empty for that layout
		// and behavior is unchanged.
		siblings := siblingGraphDirs(t, ex.Path, cfgData, outputDir)
		if *updateE2E {
			copyOutputToExpected(t, ex.Path, siblings)
			t.Logf("updated golden files for %s", ex.Name)
			return
		}
		compareWithExpected(t, ex.Path, outputDir, siblings)
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
	compareWithExpected(t, ex.Path, tmpOutput, nil)
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
// Note: this path is not used for graphql examples — those generate
// in-place at ex.Path because the chained `sqlgen graphql gen` gqlgen
// subprocess infers import paths from the example's
// go.mod, and a temp dir under /tmp has no go.mod so gqlgen collides on
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

// compareWithExpected diffs the generated output against the expected/ golden
// directory. Sibling graph dirs (outside output.dir) are folded into the
// generated set under their root-relative path so they diff against
// expected/<root-relative-path> (mirrors copyOutputToExpected).
func compareWithExpected(t *testing.T, examplePath, tmpOutputDir string, siblings map[string]string) {
	t.Helper()

	expectedDir := filepath.Join(examplePath, "expected")
	if _, err := os.Stat(expectedDir); err != nil {
		t.Fatalf("expected/ directory does not exist in %s (run with -update-e2e to create)", examplePath)
	}

	// Collect all files from both directories.
	expectedFiles := collectFiles(t, expectedDir)
	generatedFiles := collectFiles(t, tmpOutputDir)

	// Fold sibling graph trees into the generated set under their root-relative
	// path so keys line up with expected/<root-relative-path>.
	for rel, abs := range siblings {
		for k, v := range collectFiles(t, abs) {
			generatedFiles[filepath.Join(rel, k)] = v
		}
	}

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
