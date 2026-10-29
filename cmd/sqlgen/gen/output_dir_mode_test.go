package gen_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
)

// gnwOpenUmaskEnv marks a test binary re-executed under umask 000 by
// TestGenerate_CreatesMissingOutputDirectoriesWith0755.
const gnwOpenUmaskEnv = "SQLGEN_TEST_OPEN_UMASK_CHILD"

// gnwUmaskAllows returns the permission bits the process umask lets through,
// measured by creating a directory with every bit requested.
func gnwUmaskAllows(t *testing.T) fs.FileMode {
	t.Helper()
	probe := filepath.Join(t.TempDir(), "probe")
	if err := os.Mkdir(probe, 0o777); err != nil { //nolint:gosec // measuring the umask needs every bit requested
		t.Fatalf("creating umask probe: %v", err)
	}
	info, err := os.Stat(probe)
	if err != nil {
		t.Fatalf("stat umask probe: %v", err)
	}
	return info.Mode().Perm()
}

// TestGenerate_CreatesMissingOutputDirectoriesWith0755 generates into an
// output directory two levels below one that exists, and checks both
// directories generation created carry exactly mode 0o755. The usual umask
// 022 would mask a wider requested mode down to 0o755, so the check runs in
// a copy of the test binary re-executed under umask 000, where the mode
// generation requests is the mode the directory gets.
func TestGenerate_CreatesMissingOutputDirectoriesWith0755(t *testing.T) {
	const name = "TestGenerate_CreatesMissingOutputDirectoriesWith0755"
	if os.Getenv(gnwOpenUmaskEnv) == "" {
		if runtime.GOOS == "windows" {
			t.Skip("POSIX directory modes and umask do not apply on Windows")
		}
		if _, err := os.Stat("/bin/sh"); err != nil {
			t.Skipf("/bin/sh unavailable to set the umask: %v", err)
		}
		// $0 is the test binary; the shell clears the umask and replaces
		// itself with the binary, which inherits the cleared umask.
		cmd := exec.CommandContext(t.Context(), "/bin/sh", "-c", `umask 000; exec "$0" "$@"`, //nolint:gosec // re-executes this test binary with fixed arguments
			os.Args[0], "-test.run=^"+name+"$", "-test.count=1", "-test.v")
		cmd.Env = append(os.Environ(), gnwOpenUmaskEnv+"=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("test under umask 000 failed: %v\n%s", err, out)
		}
		// A run that matched no test would also exit zero.
		if !strings.Contains(string(out), "--- PASS: "+name) {
			t.Fatalf("test under umask 000 did not run:\n%s", out)
		}
		return
	}

	if allowed := gnwUmaskAllows(t); allowed != 0o777 {
		t.Fatalf("re-executed test runs under a umask that allows only %v, want all bits", allowed)
	}
	const want = fs.FileMode(0o755)

	root := t.TempDir()
	outer := filepath.Join(root, "internal")
	outDir := filepath.Join(outer, "db")
	cfg := loadGenerateConfig(t, outDir, config.DialectSQLite, "")
	schema := parseDDLForTest(t, config.DialectSQLite, `CREATE TABLE notes (id INTEGER PRIMARY KEY AUTOINCREMENT, body TEXT NOT NULL);`)
	if _, err := gen.Generate(schema, cfg, "test"); err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	for _, dir := range []string{outer, outDir} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("output directory %s was not created: %v", dir, err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("auto-created directory %s has mode %v, want %v", dir, got, want)
		}
	}
}
