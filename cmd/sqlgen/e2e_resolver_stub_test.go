package main_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2EGoldenResolvers_NoUnboundFieldStubs pins the invariant that every
// GraphQL field sqlgen emits is BOUND — readable straight off the model struct
// — and never answered by a gqlgen field-resolver stub (PRD §26.4.1).
//
// gqlgen writes such a stub whenever a schema field's type does not match the
// Go type of the model field backing it, and its body is
// `panic("not implemented: …")`. That compiles, so `make check` and
// `make check-examples` both stay green while the generated server panics on
// the first query that selects the field. Nothing else in the suite looks at
// resolver bodies, which is exactly how one shipped in the golden tree: a view
// column typed `json.RawMessage` against a `JSON` scalar bound to `types.JSON`
// (parser/view.go's aggregate table, corrected in 25.8).
//
// The check reads the committed goldens rather than generating, so it costs
// nothing and fails on the artifact a reviewer would actually read.
//
// It also rejects gqlgen's stale-resolver banner. When a stub disappears
// gqlgen preserves the old body in a commented `!!! WARNING !!!` block rather
// than deleting it, so a golden tree that once had a stub keeps a copy of it
// unless the file is removed and regenerated.
func TestE2EGoldenResolvers_NoUnboundFieldStubs(t *testing.T) {
	var offenders []string

	err := filepath.WalkDir(examplesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".resolvers.go") {
			return nil
		}
		// Only the golden copies — the live `models/graph` trees are the same
		// files, and reporting both would double every finding.
		if !strings.Contains(filepath.ToSlash(path), "/expected/") {
			return nil
		}
		src, readErr := os.ReadFile(path) //nolint:gosec // test helper walking a controlled testdata tree
		if readErr != nil {
			return readErr
		}
		body := string(src)
		rel, relErr := filepath.Rel(examplesDir, path)
		if relErr != nil {
			rel = path
		}
		if strings.Contains(body, "not implemented:") {
			offenders = append(offenders, rel+": panic stub — a schema field is not bound to its model field (PRD §26.4.1)")
		}
		if strings.Contains(body, "!!! WARNING !!!") {
			offenders = append(offenders, rel+": stale gqlgen resolver block — delete the file and regenerate so gqlgen rewrites it clean")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", examplesDir, err)
	}

	if len(offenders) > 0 {
		t.Errorf("generated resolvers must contain no unbound-field stubs:\n  %s", strings.Join(offenders, "\n  "))
	}
}
