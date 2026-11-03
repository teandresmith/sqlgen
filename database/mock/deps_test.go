package mock_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestMockImportsOnlyTheStandardLibraryAndDatabase(t *testing.T) {
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
	allowed := map[string]bool{
		"github.com/teandresmith/sqlgen/database":      true,
		"github.com/teandresmith/sqlgen/database/mock": true,
	}
	for dep := range strings.FieldsSeq(string(out)) {
		if !allowed[dep] {
			t.Errorf("dependency outside the standard library and the database package: %s", dep)
		}
	}
}
