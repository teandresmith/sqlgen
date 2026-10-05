package tests

// TestWrapperLayout pins the sqlgenresolver layout's cross-layout
// invariant: a `sqlgen graphql gen` run produces a compiling consumer
// package under BOTH `resolver.layout: follow-schema` AND `single-file`
// when chained against the real `go run github.com/99designs/gqlgen`
// subprocess. The duplicate-method-declaration defect this layout fixes
// reproduced under both layouts; this test locks both in so a future
// regression surfaces here instead of in a downstream consumer.
//
// Each layout variant runs `sqlgen graphql gen` TWICE — once to seed +
// scaffold against a fresh resolver dir, once more to exercise the
// "already-seeded, gqlgen owns the files" steady-state path.
// `go build ./...` after each run is the load-bearing assertion: a
// duplicate-method-declaration regression (the original symptom) is a
// compile-time error that `go build` surfaces directly.
//
// **Resolver-struct merge lock-in (single-file).** gqlgen v0.17.90's
// resolvergen plugin OWNS the single-file destination and rewrites the
// seeded `type Resolver struct { Client; Q; M }` back to the
// gqlgen-default `type Resolver struct{}` on every run while preserving
// the per-table method bodies. Without the post-gqlgen AST-merge, the
// preserved `r.M.X(...)` delegations fail to compile (`r.M undefined`).
// The single-file subtest exercises the merge end-to-end across two
// `sqlgen graphql gen` runs — the second run is the steady-state proof
// that the merge re-fires on every invocation, since gqlgen wipes the
// struct fields each time.
//
// Test staging cost (~30s per layout) is dominated by the chained
// gqlgen subprocess; tests run via t.Parallel against each other so
// total wall time stays bounded.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWrapperLayout_CrossLayoutCompiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping wrapper layout test in short mode")
	}

	// The example ships with `resolver.layout: follow-schema`. Each
	// subtest stages its own copy + rewrites the `resolver:` block in
	// gqlgen.yml to match the layout variant before running sqlgen
	// graphql gen.
	tests := []struct {
		name           string
		layoutVariant  string // value substituted into gqlgen.yml resolver.layout
		extraGqlgenYML string // extra YAML appended to resolver: block (single-file needs `filename:`)
	}{
		{
			name:          "follow-schema",
			layoutVariant: "follow-schema",
			// follow-schema is the example's default — no extra resolver-block keys needed.
			extraGqlgenYML: "",
		},
		{
			name:          "single-file",
			layoutVariant: "single-file",
			// Under single-file gqlgen treats `filename:` as the full
			// path to the combined resolver file, relative to its
			// working directory, and ignores `dir:`. sqlgen resolves it
			// the same way, so the usual config that names
			// the resolver directory in both keys must work: both land
			// on `models/graph/resolver.go`.
			extraGqlgenYML: "  dir: models/graph\n  filename: models/graph/resolver.go",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stage := stageGraphQLExample(t)
			rewriteGqlgenLayout(t, filepath.Join(stage, "gqlgen.yml"), tc.layoutVariant, tc.extraGqlgenYML)
			cleanLayoutSwitchArtifacts(t, stage, tc.layoutVariant)

			// First run: existing seeded files in place (gqlgen-owned
			// after the first run). The preserve-bodies invariant
			// says this should be a no-op on existing files
			// + a refresh of sqlgen-owned helpers, with the package
			// still compiling.
			if out, err := runSqlgen(t, stage, "graphql", "gen", "--config", "./sqlgen.yml"); err != nil {
				t.Fatalf("first `sqlgen graphql gen` failed: %v\n%s", err, out)
			}
			if out, err := runGoBuildAll(t, stage); err != nil {
				t.Fatalf("first `go build ./...` failed under %s: %v\n%s", tc.name, err, out)
			}

			// Second run: steady-state. The rewriter pass has nothing
			// to do (existing delegations stay verbatim), and the
			// package still compiles.
			if out, err := runSqlgen(t, stage, "graphql", "gen", "--config", "./sqlgen.yml"); err != nil {
				t.Fatalf("second `sqlgen graphql gen` failed: %v\n%s", err, out)
			}
			if out, err := runGoBuildAll(t, stage); err != nil {
				t.Fatalf("second `go build ./...` failed under %s: %v\n%s", tc.name, err, out)
			}
		})
	}
}

// rewriteGqlgenLayout rewrites the `resolver:` block in a staged
// gqlgen.yml to use the named layout. The `layout:` line is replaced in
// place; the extra block keys (e.g. `filename:` under single-file) are
// inserted immediately after, AND any existing same-keyed lines later in
// the resolver block are deleted so the override wins.
//
// Naive line-based parsing matches the example's checked-in resolver
// block shape — a single contiguous YAML mapping under the top-level
// `resolver:` key with two-space indent. The function asserts on a
// recognisable shape so a future restructure of gqlgen.yml fails the
// test loudly rather than silently bypassing the rewrite.
func rewriteGqlgenLayout(t *testing.T, path, layout, extraKeys string) {
	t.Helper()
	data := readFile(t, path)
	lines := strings.Split(string(data), "\n")

	// Parse the override keys so we know which existing lines in the
	// resolver block must be dropped to let the override win.
	overrideKeys := make(map[string]bool)
	for _, raw := range strings.Split(extraKeys, "\n") {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		colon := strings.Index(trimmed, ":")
		if colon < 0 {
			continue
		}
		overrideKeys[strings.TrimSpace(trimmed[:colon])] = true
	}

	var out []string
	layoutFound := false
	inResolver := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "resolver:" {
			inResolver = true
			out = append(out, line)
			continue
		}
		// Leaving the resolver block when we hit a non-indented non-empty line.
		if inResolver && len(trimmed) > 0 && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			inResolver = false
		}
		// Replace `layout:` in place, then inject the override keys
		// directly after so they stay part of the resolver mapping.
		if inResolver && !layoutFound && strings.HasPrefix(trimmed, "layout:") {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			out = append(out, indent+"layout: "+layout)
			if extraKeys != "" {
				out = append(out, strings.TrimRight(extraKeys, "\n"))
			}
			layoutFound = true
			continue
		}
		// Drop any other key in the resolver block that the override
		// supersedes (e.g. `dir:` when extraKeys sets `dir:`).
		if inResolver && len(trimmed) > 0 {
			if colon := strings.Index(trimmed, ":"); colon > 0 {
				key := strings.TrimSpace(trimmed[:colon])
				if overrideKeys[key] {
					continue
				}
			}
		}
		out = append(out, line)
	}
	if !layoutFound {
		t.Fatalf("rewriteGqlgenLayout: no `layout:` line found in resolver block of %s", path)
	}
	if err := os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// cleanLayoutSwitchArtifacts removes resolver files left over from the
// example's checked-in `follow-schema` layout when the test runs under a
// different layout. The example ships with per-schema `*_gen.resolvers.go`
// files plus the slim `models/graph/resolver.go` scaffold (all targeting
// follow-schema). Under `single-file` gqlgen emits Resolver +
// queryResolver + mutationResolver + every per-field method into ONE
// combined file at `models/graph/resolver.go`; the leftover per-schema
// files would then declare duplicates of those methods and the package
// would fail to compile with `<Method> redeclared in this block`.
//
// This mirrors what a consumer would do manually when intentionally
// switching `resolver.layout` — sqlgen does not auto-clean across layout
// switches (PRD §26.5.6 keeps the consumer in control of the resolver
// dir's file layout).
func cleanLayoutSwitchArtifacts(t *testing.T, stage, layout string) {
	t.Helper()
	if layout == "follow-schema" {
		return
	}
	resolverDir := filepath.Join(stage, "models", "graph")
	matches, err := filepath.Glob(filepath.Join(resolverDir, "*_gen.resolvers.go"))
	if err != nil {
		t.Fatalf("globbing follow-schema resolver files: %v", err)
	}
	matches = append(matches, filepath.Join(resolverDir, "resolver.go"))
	for _, path := range matches {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			t.Fatalf("removing %s before %s run: %v", path, layout, err)
		}
	}
}

// runGoBuildAll runs `go build ./...` inside the staged consumer dir and
// returns combined stdout+stderr for diagnostics. GOWORK=off keeps the
// build scoped to the staged module — without it, go.work would try to
// claim the staged dir as part of the surrounding workspace, which is
// not its actual home.
func runGoBuildAll(t *testing.T, dir string) (string, error) {
	t.Helper()
	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}
