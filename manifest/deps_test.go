package manifest_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestManifestPackage_StdlibOnly asserts the runtime manifest package imports
// only the standard library, honoring the runtime dependency invariant in PRD
// §3 (this package is embedded into every consumer binary via manifest_embed_gen.go).
func TestManifestPackage_StdlibOnly(t *testing.T) {
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

	self := "github.com/teandresmith/sqlgen/manifest"
	for dep := range strings.FieldsSeq(string(out)) {
		if dep == self {
			continue
		}
		// Standard-library import paths have no dotted domain in their first
		// path segment; any external module does (e.g. github.com/...).
		first, _, _ := strings.Cut(dep, "/")
		if strings.Contains(first, ".") {
			t.Errorf("non-stdlib dependency: %s", dep)
		}
	}
}
