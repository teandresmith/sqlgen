package cli

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The chained `sqlgen generate` → gqlgen wrapper path (PRD §26.5.6
// "Pre-flight" and "Failure mode"), driven end to end through the CLI with a
// stand-in gqlgen binary.

// twrGraphQLProject lays out a temp project (sqlgen.yml, one migration, an
// optional gqlgen.yml) and chdirs into it, since schema_dir / resolver_dir
// are root-relative (§26.5.8). It returns the sqlgen.yml and output paths.
func twrGraphQLProject(t *testing.T, ddl, gqlgenYML, gqlgenConfigPath string) (cfgPath, outDir string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath = filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	outDir = filepath.Join(dir, "output")
	if gqlgenYML != "" {
		if err := os.WriteFile(filepath.Join(dir, "gqlgen.yml"), []byte(gqlgenYML), 0o600); err != nil {
			t.Fatalf("writing gqlgen.yml: %v", err)
		}
	}
	t.Chdir(dir)
	writeAPIGraphQLConfig(t, cfgPath, sqlDir, outDir, gqlgenConfigPath)
	writeSQLFile(t, sqlDir, ddl)
	return cfgPath, outDir
}

// twrStandInGqlgen writes a POSIX shell script that stands in for the gqlgen
// binary and returns the gqlgen_bin value that runs it.
func twrStandInGqlgen(t *testing.T, body string) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "gqlgen.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"+body), 0o600); err != nil {
		t.Fatalf("writing stand-in gqlgen: %v", err)
	}
	return "/bin/sh " + script
}

const twrFollowSchemaGqlgenYML = "schema:\n  - graph/*.graphqls\nresolver:\n  layout: follow-schema\n  dir: graph\n  package: graph\n"

// TestGenerate_ChainedGraphQL_PreflightDoesNotScaffold pins "Auto-init is
// deliberately **not** a side effect of generate": a missing gqlgen.yml fails
// the run and leaves no gqlgen.yml behind, at the default path or at a
// mistyped configured one.
func TestGenerate_ChainedGraphQL_PreflightDoesNotScaffold(t *testing.T) {
	tests := []struct {
		name       string
		configured string // api.graphql.gqlgen_config; "" uses the default ./gqlgen.yml
		missing    string // path, relative to the project root, that must stay absent
	}{
		{name: "default path stays absent", configured: "", missing: "gqlgen.yml"},
		{name: "mistyped configured path stays absent", configured: "./gqlgn.yml", missing: "gqlgn.yml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgPath, _ := twrGraphQLProject(t, "CREATE TABLE products (id uuid PRIMARY KEY, name text NOT NULL);", "", tt.configured)

			_, stderr, err := executeCommand("--config", cfgPath)
			if err == nil {
				t.Fatal("generate with no gqlgen.yml returned nil error")
			}
			if !strings.Contains(stderr, "sqlgen graphql init") {
				t.Fatalf("stderr = %q, want the pre-flight error pointing at sqlgen graphql init", stderr)
			}
			for _, p := range []string{tt.missing, "gqlgen.yml"} {
				if _, statErr := os.Stat(p); !errors.Is(statErr, os.ErrNotExist) {
					t.Errorf("generate left %s on disk (stat err %v); a gqlgen.yml must only come from sqlgen graphql init", p, statErr)
				}
			}
		})
	}
}

// TestGenerate_ChainedGraphQL_SubprocessFailureKeepsArtifacts pins "If the
// gqlgen subprocess fails, the sqlgen-side files emitted earlier in the run
// stay on disk and are **not** rolled back."
func TestGenerate_ChainedGraphQL_SubprocessFailureKeepsArtifacts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stand-in gqlgen is a POSIX shell script")
	}
	cfgPath, outDir := twrGraphQLProject(t, "CREATE TABLE products (id uuid PRIMARY KEY, name text NOT NULL);", twrFollowSchemaGqlgenYML, "")
	appendConfig(t, cfgPath, "    gqlgen_bin: "+twrStandInGqlgen(t, "echo 'gqlgen: validation failed' >&2\nexit 1\n")+"\n")

	_, stderr, err := executeCommand("--config", cfgPath)
	if err == nil {
		t.Fatal("generate with a failing gqlgen subprocess returned nil error")
	}
	if !strings.Contains(stderr, "Error: gqlgen subprocess failed: ") {
		t.Fatalf("stderr = %q, want the gqlgen subprocess failure", stderr)
	}

	models, globErr := filepath.Glob(filepath.Join(outDir, "*_gen.go"))
	if globErr != nil {
		t.Fatalf("globbing %s: %v", outDir, globErr)
	}
	if len(models) == 0 {
		t.Errorf("no *_gen.go files remain in %s after the subprocess failed; sqlgen-side artifacts must not be rolled back", outDir)
	}
	for _, f := range models {
		if info, statErr := os.Stat(f); statErr != nil || info.Size() == 0 {
			t.Errorf("%s is missing or empty after the subprocess failed (stat err %v)", f, statErr)
		}
	}
	for _, p := range []string{
		filepath.Join("graph", "product_gen.graphqls"),
		filepath.Join("graph", "shared_gen.graphqls"),
	} {
		if _, statErr := os.Stat(p); statErr != nil {
			t.Errorf("%s is gone after the subprocess failed: %v", p, statErr)
		}
	}
}

// twrRecasedOnlyPKStub is the resolver file gqlgen writes for an `only_pks`
// table: it names the root resolver OnlyPk where sqlgen's seed says OnlyPK,
// and leaves a panic stub under its own spelling.
const twrRecasedOnlyPKStub = `package graph

import (
	"context"
	"fmt"
)

// OnlyPk is the resolver for the onlyPK field.
func (r *queryResolver) OnlyPk(ctx context.Context, id int64) (*int, error) {
	panic(fmt.Errorf("not implemented: OnlyPk - onlyPK"))
}
`

// TestGenerate_ChainedGraphQL_UnboundRootFieldOwnError pins two §26.5.6
// promises for a managed root field gqlgen spells differently from sqlgen:
// "an unbound root resolver name prints its own error" — not the subprocess
// or wrapper label with the artifacts-emitted suffix — and "generation fails
// (exit `1`)".
func TestGenerate_ChainedGraphQL_UnboundRootFieldOwnError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stand-in gqlgen is a POSIX shell script")
	}
	cfgPath, _ := twrGraphQLProject(t, "CREATE TABLE only_pks (id bigint PRIMARY KEY, label text NOT NULL);", twrFollowSchemaGqlgenYML, "")
	fixture := filepath.Join(t.TempDir(), "only_pk_gen.resolvers.go")
	if err := os.WriteFile(fixture, []byte(twrRecasedOnlyPKStub), 0o600); err != nil {
		t.Fatalf("writing stub fixture: %v", err)
	}
	// The stand-in replaces the seeded resolver file the way gqlgen does when
	// it re-cases a root resolver name, then succeeds.
	bin := twrStandInGqlgen(t, "cp '"+fixture+"' graph/only_pk_gen.resolvers.go\n")
	appendConfig(t, cfgPath, "    gqlgen_bin: "+bin+"\n")

	_, stderr, err := executeCommand("--config", cfgPath)
	if err == nil {
		t.Fatal("generate with an unbound managed root field returned nil error")
	}

	t.Run("prints its own error", func(t *testing.T) {
		_, after, found := strings.Cut(stderr, "Error: ")
		if !found {
			t.Fatalf("stderr = %q, want an \"Error: \" line", stderr)
		}
		if !strings.HasPrefix(after, "gqlgen named ") {
			t.Errorf("error line = %q, want the unbound-root-field error", "Error: "+after)
		}
		for _, label := range []string{"gqlgen subprocess failed", "gqlgen wrapper:", "sqlgen-side artifacts emitted at"} {
			if strings.Contains(stderr, label) {
				t.Errorf("stderr carries %q; the unbound-root-field failure prints its own error:\n%s", label, stderr)
			}
		}
		if !strings.Contains(stderr, "tables.only_pks.struct_name") {
			t.Errorf("stderr does not name the struct_name remedy:\n%s", stderr)
		}
	})

	t.Run("exits 1", func(t *testing.T) {
		// Execute maps the command error to the process exit status this way.
		code, ok := exitCodeFromError(err)
		if !ok {
			t.Fatalf("err = %T %v carries no exit code", err, err)
		}
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
	})
}
