package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeManifestConfig writes a minimal sqlite config whose manifest block is
// enabled or not, into dir.
func writeManifestConfig(t *testing.T, cfgPath, sqlDir, outDir string, manifestEnabled bool) {
	t.Helper()
	body := "version: v1\n" +
		"input:\n  dialect: sqlite\n  paths:\n    - " + filepath.ToSlash(sqlDir) + "\n" +
		"output:\n  driver: stdlib\n  dir: " + filepath.ToSlash(outDir) + "\n  package: models\n" +
		"generation:\n  manifest:\n    enabled: " + map[bool]string{true: "true", false: "false"}[manifestEnabled] + "\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
}

// TestGenerate_ReportsManifestDeletions pins manifest-deletion reporting at
// the CLI level. The unit tests assert manifest.StageResult.Deleted; this asserts
// the CLI actually surfaces it, which is the part a consumer sees. Before the
// fix the manifest directory, the breadcrumbs and the embed file were all
// removed without appearing anywhere in the output.
func TestGenerate_ReportsManifestDeletions(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	outDir := filepath.Join(dir, "models")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	writeSQLFile(t, sqlDir, "CREATE TABLE products (id integer PRIMARY KEY, name text NOT NULL);")

	// Enabled run lays down the manifest surface.
	writeManifestConfig(t, cfgPath, sqlDir, outDir, true)
	if _, stderr, err := executeCommand("--config", cfgPath); err != nil {
		t.Fatalf("enabled generate failed: %v\nstderr: %s", err, stderr)
	}
	if _, err := os.Stat(filepath.Join(outDir, "manifest")); err != nil {
		t.Fatalf("enabled run did not write the manifest dir: %v", err)
	}

	// Flip to disabled: the sweep runs, and every removal must be named.
	writeManifestConfig(t, cfgPath, sqlDir, outDir, false)
	stdout, stderr, err := executeCommand("--config", cfgPath, "--verbose")
	if err != nil {
		t.Fatalf("disabled generate failed: %v\nstderr: %s", err, stderr)
	}

	// The directory is reported with a trailing "/" so a reader can tell it
	// from the files listed beside it.
	wantDir := "deleted: " + filepath.ToSlash(filepath.Join(outDir, "manifest")) + "/"
	for _, want := range []string{
		wantDir,
		"deleted: " + filepath.ToSlash(filepath.Join(outDir, "CLAUDE.md")),
		"deleted: " + filepath.ToSlash(filepath.Join(outDir, "manifest_embed_gen.go")),
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("generate --verbose output missing %q\n--- stdout ---\n%s", want, stdout)
		}
	}

	if _, err := os.Stat(filepath.Join(outDir, "manifest")); !os.IsNotExist(err) {
		t.Errorf("manifest dir survived the flip to disabled (stat err = %v)", err)
	}
}

// TestGenerate_UnownedManifestDirSurvivesCLI reproduces the stale sweep
// deleting a manifest/ directory it did not own, through the binary's own
// entry point rather than through CleanStale: a
// consumer package that happens to be named manifest/, sitting in output.dir,
// with no manifest block in the config at all — the default shape of every
// `sqlgen generate` run.
func TestGenerate_UnownedManifestDirSurvivesCLI(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	outDir := filepath.Join(dir, "models")
	consumerDir := filepath.Join(outDir, "manifest", "internal")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	if err := os.MkdirAll(consumerDir, 0o750); err != nil {
		t.Fatalf("creating consumer manifest dir: %v", err)
	}
	consumerFile := filepath.Join(consumerDir, "load.go")
	if err := os.WriteFile(consumerFile, []byte("package internal\n"), 0o600); err != nil {
		t.Fatalf("seeding consumer file: %v", err)
	}
	writeSQLFile(t, sqlDir, "CREATE TABLE products (id integer PRIMARY KEY, name text NOT NULL);")

	// No `generation:` block at all — the manifest is off by default, which is
	// the path that used to RemoveAll the directory.
	body := "version: v1\n" +
		"input:\n  dialect: sqlite\n  paths:\n    - " + filepath.ToSlash(sqlDir) + "\n" +
		"output:\n  driver: stdlib\n  dir: " + filepath.ToSlash(outDir) + "\n  package: models\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	stdout, stderr, err := executeCommand("--config", cfgPath, "--verbose")
	if err != nil {
		t.Fatalf("generate failed: %v\nstderr: %s", err, stderr)
	}

	if _, err := os.Stat(consumerFile); err != nil {
		t.Fatalf("sqlgen generate destroyed a consumer-owned manifest/ directory: %v", err)
	}
	if strings.Contains(stdout, "deleted: "+filepath.ToSlash(filepath.Join(outDir, "manifest"))) {
		t.Errorf("generate reported deleting a directory it does not own\n--- stdout ---\n%s", stdout)
	}
}
