package cli

import (
	"maps"
	"slices"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// TestBuildMergeInput_BareBuiltinScalarIsInert pins PRD §26.4.1: "An entry
// declaring `marshaling: builtin` with no `go_type` binds nothing to check —
// it reaches no column, declares no `scalar`, and emits no `models:` entry".
// The schema-side half (no `scalar` declaration) is pinned by the gen
// package's TestBareBuiltinScalar_isInert.
func TestBuildMergeInput_BareBuiltinScalarIsInert(t *testing.T) {
	decls := map[string]config.ScalarBinding{
		"IPAddr":       consumerScalarDecls["IPAddr"],
		"EmailAddress": consumerScalarDecls["EmailAddress"],
		"Any":          {Marshaling: config.ScalarMarshalingBuiltin},
	}
	cfg := newConsumerScalarConfig(decls)
	apiCtx := buildConsumerScalarAPICtx(t, cfg)

	for _, s := range apiCtx.UsedScalars {
		if s.Name == "Any" {
			t.Errorf("a bare builtin entry reached a column: UsedScalars carries %+v", s)
		}
	}

	merge, _ := buildMergeInput(cfg, apiCtx, "example.com/foo")
	if paths, ok := merge.Models["Any"]; ok {
		t.Errorf("merge.Models[%q] = %v, want no entry for a bare builtin declaration", "Any", paths)
	}
	// Control: the declared scalars a column does resolve to are merged.
	for _, name := range []string{"IPAddr", "EmailAddress"} {
		if _, ok := merge.Models[name]; !ok {
			t.Errorf("merge.Models lacks %q, which a column resolves to; have %v", name, slices.Sorted(maps.Keys(merge.Models)))
		}
	}
}
