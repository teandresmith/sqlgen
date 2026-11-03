package database_test

import (
	"os/exec"
	"strings"
	"testing"
)

// nonStandardDeps returns the import paths outside the standard library that
// the package in the current directory depends on, itself included.
func nonStandardDeps(t *testing.T) []string {
	t.Helper()
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH; skipping deps check")
	}
	// goBin is resolved from PATH via exec.LookPath and every argument is a
	// constant, so this is not an injection surface.
	out, err := exec.Command(goBin, "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", ".").Output() //nolint:gosec // G204: constant args, trusted go binary
	if err != nil {
		t.Fatalf("go list -deps: %v", err)
	}
	return strings.Fields(string(out))
}

func TestDatabasePackageImportsOnlyTheStandardLibrary(t *testing.T) {
	const self = "github.com/teandresmith/sqlgen/database"
	for _, dep := range nonStandardDeps(t) {
		if dep != self {
			t.Errorf("non-stdlib dependency: %s", dep)
		}
	}
}
