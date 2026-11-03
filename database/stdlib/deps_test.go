package stdlib_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestStdlibAdapterNeverImportsPgx(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH; skipping deps check")
	}
	// goBin is resolved from PATH via exec.LookPath and every argument is a
	// constant, so this is not an injection surface.
	out, err := exec.Command(goBin, "list", "-deps", ".").Output() //nolint:gosec // G204: constant args, trusted go binary
	if err != nil {
		t.Fatalf("go list -deps: %v", err)
	}
	for dep := range strings.FieldsSeq(string(out)) {
		if strings.HasPrefix(dep, "github.com/jackc/") {
			t.Errorf("pgx dependency: %s", dep)
		}
	}
}
