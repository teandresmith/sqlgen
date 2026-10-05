package tests

// Shared infrastructure for the wrapper-regression tests:
//
//	wrapper_layered_test.go  — consumer gqlgen.yml byte-equal pre/post + merged-config capture
//	wrapper_layout_test.go   — cross-layout lock-in (follow-schema + single-file)
//	wrapper_rewriter_test.go — panic-stub rewriter lock-in (panic-stub → delegation flip)
//
// The walker_lint_test.go file is a static check over the in-tree generated
// `field_options_gen.go` and does not need the staging machinery here.
//
// These tests stage a copy of the graphql example dir into `t.TempDir()`
// because they invoke `sqlgen graphql gen` with config flips that would
// otherwise mutate the in-tree fixture (and race with the other tests in
// this package). The staged copy retains the example's `go.mod` / `go.sum`,
// whose gqlgen tool directive lets the chained `go run github.com/99designs/gqlgen`
// subprocess resolve its build deps; the `replace` directive in go.mod is rewritten to
// an absolute path to the actual sqlgen repo root (the example's relative
// `../../../../..` no longer points anywhere useful from `t.TempDir()`).

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// sqlgenBin holds the path to the sqlgen binary built once per test process.
// Built lazily by buildSqlgenOnce so individual tests that don't need to
// subprocess sqlgen don't pay the build cost. Built with GOWORK pointing at
// the repo's workspace so the parser/runtime modules resolve; invoked with
// GOWORK=off so the chained gqlgen subprocess works against the staged
// fixture's `go.mod` rather than the workspace it doesn't belong to.
//
// sqlgenBinDir is the parent temp dir of sqlgenBin. Tracked separately from
// sqlgenBin so cleanupSqlgenBuild can remove the dir even when the build
// failed after MkdirTemp succeeded (sqlgenBin stays empty on build error).
var (
	sqlgenBin      string
	sqlgenBinDir   string
	sqlgenBuildErr error
	sqlgenBuild    sync.Once
)

// repoRoot returns the absolute path to the sqlgen repository root, derived
// from the test file's compile-time location. The test file lives at
// `<repo>/cmd/sqlgen/testdata/examples/graphql/tests/`, so the root is five
// levels up. Computed via runtime.Caller so the location is robust under any
// working directory the test runner picks.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("repoRoot: runtime.Caller(0) failed")
	}
	// file = <repo>/cmd/sqlgen/testdata/examples/graphql/tests/wrapper_fixture_test.go
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "..", ".."))
}

// buildSqlgenOnce compiles the sqlgen CLI into a per-process temp dir and
// stashes the binary path in `sqlgenBin`. Returns the cached binary on
// subsequent calls. Builds with the repo's workspace in scope (no GOWORK
// override) so the parser/runtime dependencies resolve through `go.work`.
func buildSqlgenOnce(t *testing.T) string {
	t.Helper()
	sqlgenBuild.Do(func() {
		root := repoRoot(t)
		binDir, err := os.MkdirTemp("", "sqlgen-bin-*")
		if err != nil {
			sqlgenBuildErr = fmt.Errorf("creating bin dir: %w", err)
			return
		}
		sqlgenBinDir = binDir
		bin := filepath.Join(binDir, "sqlgen")
		cmd := exec.Command("go", "build", "-o", bin, ".")
		cmd.Dir = filepath.Join(root, "cmd", "sqlgen")
		// The build must see the repo's go.work so the cmd/sqlgen module
		// can resolve its sibling parser/runtime imports. `make
		// test-examples` sets GOWORK=off (because the example modules
		// aren't part of the workspace), but the sqlgen build itself is
		// rooted INSIDE the workspace at cmd/sqlgen, so we strip the
		// inherited override and let Go auto-discover go.work.
		cmd.Env = envWithout(os.Environ(), "GOWORK")
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			sqlgenBuildErr = fmt.Errorf("building sqlgen: %w\n%s", err, out.String())
			return
		}
		sqlgenBin = bin
	})
	if sqlgenBuildErr != nil {
		t.Fatal(sqlgenBuildErr)
	}
	return sqlgenBin
}

// runSqlgen invokes the cached sqlgen binary inside the given working
// directory with the supplied args. Returns combined stdout+stderr for
// failure diagnostics. GOWORK=off is enforced so the chained gqlgen
// subprocess (when it runs) resolves its deps from the staged go.mod rather
// than walking up into the repo's workspace.
func runSqlgen(t *testing.T, workdir string, args ...string) (string, error) {
	t.Helper()
	bin := buildSqlgenOnce(t)
	cmd := exec.Command(bin, args...)
	cmd.Dir = workdir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	cmd.Env = append(os.Environ(), "GOWORK=off")
	err := cmd.Run()
	return out.String(), err
}

// stageGraphQLExample copies the in-tree graphql example dir into t.TempDir,
// rewriting the `replace github.com/teandresmith/sqlgen` line in go.mod to an
// absolute path. The model/ tree (already-generated artifacts) is included so
// chained sqlgen + gqlgen subprocesses can reuse the existing seed files and
// the staged dir is build-ready out of the gate.
//
// The `expected/` directory (golden output for the existing E2E harness)
// and the `tests/` directory (this test package) are excluded — they're
// not part of the live consumer build graph, and including them would
// pollute `go build ./...` checks with snapshots that don't share state
// with the post-staging tree.
//
// Returns the absolute path to the staged fixture root.
func stageGraphQLExample(t *testing.T) string {
	t.Helper()
	src := filepath.Dir(exampleDir(t))
	dst := t.TempDir()
	if err := copyDirRecursive(src, dst, "expected", "tests"); err != nil {
		t.Fatalf("staging example fixture: %v", err)
	}
	rewriteReplaceLine(t, filepath.Join(dst, "go.mod"), repoRoot(t))
	return dst
}

// exampleDir returns the absolute path to the in-tree graphql example's
// `tests/` directory — the parent of `wrapper_fixture_test.go`. Callers that
// want the example root (the parent of `tests/`) pass it through filepath.Dir.
func exampleDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("exampleDir: runtime.Caller(0) failed")
	}
	return filepath.Dir(file)
}

// copyDirRecursive copies every file and directory from src to dst,
// pruning any top-level directories whose names appear in skipDirs.
// Symlinks are resolved (not preserved) — the staged fixture is a flat
// snapshot, not a live link into the source tree. File permissions are
// preserved within the safe 0o600/0o700 envelope.
func copyDirRecursive(src, dst string, skipDirs ...string) error {
	skip := make(map[string]bool, len(skipDirs))
	for _, d := range skipDirs {
		skip[d] = true
	}
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		// Skip pruned dirs and everything under them. Match by top-level
		// path component so e.g. `expected` matches `expected/graph/...`
		// but not `models/expected_fixtures`.
		if rel != "." {
			first, _, _ := strings.Cut(rel, string(filepath.Separator))
			if skip[first] {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		return copyFile(path, target)
	})
}

// copyFile copies a single file's bytes from src to dst, creating parent
// directories as needed.
func copyFile(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // staged fixture path under t.TempDir
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// rewriteReplaceLine rewrites every `replace github.com/teandresmith/sqlgen[/...]
// => <relative>` directive in a staged go.mod to point at an absolute path
// under absRepoRoot. The example's checked-in replaces use relative paths
// that only resolve from the in-tree location; once staged to `t.TempDir()`
// those paths no longer reach the repo root, so we substitute absolutes.
//
// Replaces handled:
//   - `github.com/teandresmith/sqlgen => ../../../../..` → absRepoRoot
//   - `github.com/teandresmith/sqlgen/cache/memory => ../../../../../cache/memory`
//     → absRepoRoot/cache/memory
func rewriteReplaceLine(t *testing.T, modPath, absRepoRoot string) {
	t.Helper()
	data, err := os.ReadFile(modPath) //nolint:gosec // path is staged go.mod under t.TempDir
	if err != nil {
		t.Fatalf("reading %s: %v", modPath, err)
	}
	type rewrite struct {
		needle string
		target string
	}
	rewrites := []rewrite{
		{"replace github.com/teandresmith/sqlgen => ", absRepoRoot},
		{"replace github.com/teandresmith/sqlgen/cache/memory => ", filepath.Join(absRepoRoot, "cache", "memory")},
	}
	lines := strings.Split(string(data), "\n")
	found := make([]bool, len(rewrites))
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		for j, rw := range rewrites {
			if strings.HasPrefix(trimmed, rw.needle) {
				lines[i] = rw.needle + rw.target
				found[j] = true
				break
			}
		}
	}
	for j, ok := range found {
		if !ok {
			t.Fatalf("rewriteReplaceLine: no `%s` line in %s", rewrites[j].needle, modPath)
		}
	}
	if err := os.WriteFile(modPath, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatalf("writing %s: %v", modPath, err)
	}
}

// readFile is a thin wrapper around os.ReadFile that t.Fatals on error.
// Centralises the //nolint:gosec annotation tests need for staged-path reads.
func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // path is staged fixture under t.TempDir or repo-rooted source
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return b
}

// cleanupSqlgenBuild removes the per-process temp dir created by
// buildSqlgenOnce. Safe to call when the build never ran (sqlgenBinDir
// empty) or when only MkdirTemp succeeded (binary path stayed empty but
// the dir is still on disk). Invoked from TestMain after m.Run so the
// dir's lifecycle matches the test process.
func cleanupSqlgenBuild() {
	if sqlgenBinDir == "" {
		return
	}
	_ = os.RemoveAll(sqlgenBinDir)
}

// envWithout returns a copy of env with every entry starting with key=
// removed. Used by buildSqlgenOnce to strip GOWORK=off so the sqlgen
// build resolves through the repo's go.work — without disturbing the
// parent test process's env (which still wants GOWORK=off so the staged
// example dirs aren't claimed by the workspace).
func envWithout(env []string, key string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env))
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			continue
		}
		out = append(out, e)
	}
	return out
}
