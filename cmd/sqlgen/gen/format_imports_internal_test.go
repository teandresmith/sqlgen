package gen

import (
	"slices"
	"testing"
)

// TestResolvePackageImports_BackfillsAndPrunes locks the two-pass import
// resolution the file_per_table layout depends on: goimports back-fills the
// package imports a template body references but the seed context does not
// declare (context and comparator — the shape that made file_per_table output
// fail to compile), and prunes a declared-but-unused seed import.
func TestResolvePackageImports_BackfillsAndPrunes(t *testing.T) {
	// Uses context and comparator — neither declared in the seed — plus a
	// declared-but-unused seed import (fmt) that must be pruned.
	body := []byte(`func query(ctx context.Context) *comparator.String {
	_ = ctx
	return nil
}`)
	seed := []string{"fmt"}

	got, err := resolvePackageImports("models", seed, body, "v0.0.0-test", "models_gen.go")
	if err != nil {
		t.Fatalf("resolvePackageImports: %v", err)
	}

	for _, want := range []string{
		"context",
		"github.com/teandresmith/sqlgen/comparator",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("resolved imports %v missing back-filled %q", got, want)
		}
	}
	if slices.Contains(got, "fmt") {
		t.Errorf("unused seed import %q should have been pruned: %v", "fmt", got)
	}
}
