package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// TestResolveManifestTimestamp pins the --manifest-timestamp resolution
// (deferred from 18.3): an explicit flag value is used verbatim (pinning
// generated_at for deterministic manifest output), and an unset flag falls back
// to a valid RFC3339 UTC timestamp.
func TestResolveManifestTimestamp(t *testing.T) {
	const fixed = "2026-07-09T12:34:56Z"
	if got := resolveManifestTimestamp(fixed); got != fixed {
		t.Errorf("resolveManifestTimestamp(%q) = %q, want verbatim", fixed, got)
	}

	got := resolveManifestTimestamp("")
	parsed, err := time.Parse(time.RFC3339, got)
	if err != nil {
		t.Fatalf("fallback timestamp %q is not RFC3339: %v", got, err)
	}
	if loc := parsed.Location(); loc != time.UTC {
		t.Errorf("fallback timestamp %q is not UTC (location %v)", got, loc)
	}
}

// TestShouldChainGraphQLGen pins the chaining gate: `sqlgen generate`
// runs the gqlgen wrapper inline iff api.graphql.enabled and the consumer
// hasn't opted out via --no-graphql or SQLGEN_NO_GRAPHQL.
func TestShouldChainGraphQLGen(t *testing.T) {
	tests := []struct {
		name        string
		apiEnabled  bool
		gqlEnabled  bool
		apiNil      bool
		gqlNil      bool
		flagNoGQL   bool
		envNoGQL    string
		wantChained bool
	}{
		{name: "api+graphql enabled, no opt-out", apiEnabled: true, gqlEnabled: true, wantChained: true},
		{name: "api disabled", apiEnabled: false, gqlEnabled: true, wantChained: false},
		{name: "graphql disabled", apiEnabled: true, gqlEnabled: false, wantChained: false},
		{name: "api block nil", apiNil: true, wantChained: false},
		{name: "graphql block nil", apiEnabled: true, gqlNil: true, wantChained: false},
		{name: "--no-graphql flag honored", apiEnabled: true, gqlEnabled: true, flagNoGQL: true, wantChained: false},
		{name: "SQLGEN_NO_GRAPHQL=1 honored", apiEnabled: true, gqlEnabled: true, envNoGQL: "1", wantChained: false},
		{name: "SQLGEN_NO_GRAPHQL=true honored", apiEnabled: true, gqlEnabled: true, envNoGQL: "true", wantChained: false},
		{name: "SQLGEN_NO_GRAPHQL empty does not opt out", apiEnabled: true, gqlEnabled: true, envNoGQL: "", wantChained: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.RootConfig{}
			if !tt.apiNil {
				cfg.API = &config.APIConfig{Enabled: tt.apiEnabled}
				if !tt.gqlNil {
					cfg.API.GraphQL = &config.GraphQLAPIConfig{Enabled: tt.gqlEnabled}
				}
			}
			flags := &cliFlags{noGraphQL: tt.flagNoGQL}
			t.Setenv("SQLGEN_NO_GRAPHQL", tt.envNoGQL)
			got := shouldChainGraphQLGen(cfg, flags)
			if got != tt.wantChained {
				t.Errorf("shouldChainGraphQLGen = %v, want %v", got, tt.wantChained)
			}
		})
	}
}

// TestGenerate_ChainedGraphQL_MissingGqlgenYml pins the pre-flight
// check: when api.graphql.enabled is true but gqlgen.yml is missing,
// `sqlgen generate` returns a helpful error pointing at `sqlgen graphql init`.
// The generate run still emits its own files (models, schema, translators)
// because the chained gqlgen call only fires after step 7 — so the failure
// surfaces loudly without rolling back partial generation.
func TestGenerate_ChainedGraphQL_MissingGqlgenYml(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	outDir := filepath.Join(dir, "output")
	// §26.5.8: schema_dir / resolver_dir are root-relative — resolved against
	// the cwd (the module root sqlgen runs in), not output.dir. Run from the
	// temp project root so the generated graph package lands under it rather
	// than in the cli package dir. (generate emits graph before the chained
	// gqlgen pre-flight fails, so the config's relative ./graph is exercised.)
	t.Chdir(dir)
	writeAPIGraphQLConfig(t, cfgPath, sqlDir, outDir, "")
	writeSQLFile(t, sqlDir, "CREATE TABLE products (id uuid PRIMARY KEY, name text NOT NULL);")

	_, stderr, err := executeCommand("--config", cfgPath)
	if err == nil {
		t.Fatalf("expected error when gqlgen.yml is missing, got nil")
	}

	var ee *exitError
	if !errors.As(err, &ee) {
		t.Fatalf("expected *exitError, got %T: %v", err, err)
	}
	if ee.code != ExitConfig {
		t.Errorf("expected exit code %d (config error), got %d: %v", ExitConfig, ee.code, err)
	}
	if !strings.Contains(stderr, "sqlgen graphql init") {
		t.Errorf("stderr should suggest 'sqlgen graphql init', got: %q", stderr)
	}
	if !strings.Contains(stderr, "gqlgen.yml") {
		t.Errorf("stderr should reference gqlgen.yml in the missing-config error, got: %q", stderr)
	}
}

// TestGenerate_ChainedGraphQL_NoGraphQLFlag pins the escape hatch:
// `sqlgen generate --no-graphql` skips the chained gqlgen call even when
// api.graphql.enabled is true. The check: a missing gqlgen.yml does NOT
// trigger the pre-flight error, because the chain never runs.
func TestGenerate_ChainedGraphQL_NoGraphQLFlag(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	outDir := filepath.Join(dir, "output")
	// §26.5.8: schema_dir / resolver_dir are root-relative — resolved against
	// the cwd (the module root sqlgen runs in), not output.dir. Run from the
	// temp project root so the generated graph package lands under it rather
	// than in the cli package dir. (generate emits graph before the chained
	// gqlgen pre-flight fails, so the config's relative ./graph is exercised.)
	t.Chdir(dir)
	writeAPIGraphQLConfig(t, cfgPath, sqlDir, outDir, "")
	writeSQLFile(t, sqlDir, "CREATE TABLE products (id uuid PRIMARY KEY, name text NOT NULL);")

	_, stderr, err := executeCommand("--config", cfgPath, "--no-graphql")
	if err != nil {
		// Generation might still error on its own (not all paths produce a
		// passing run with this minimal config). What we care about: it
		// must NOT be the chained-graphql pre-flight error pointing at sqlgen
		// graphql init — the flag short-circuits before that check.
		if strings.Contains(stderr, "sqlgen graphql init") {
			t.Errorf("--no-graphql must skip the chained gqlgen call; pre-flight error fired anyway:\n%s", stderr)
		}
	}
}

// TestGenerate_ChainedGraphQL_EnvVar mirrors TestGenerate_ChainedGraphQL_NoGraphQLFlag
// for the SQLGEN_NO_GRAPHQL=1 escape hatch — same effect via env so CI
// pipelines with a separate gqlgen step don't have to thread a flag through
// every `go generate` invocation.
func TestGenerate_ChainedGraphQL_EnvVar(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	outDir := filepath.Join(dir, "output")
	// §26.5.8: schema_dir / resolver_dir are root-relative — resolved against
	// the cwd (the module root sqlgen runs in), not output.dir. Run from the
	// temp project root so the generated graph package lands under it rather
	// than in the cli package dir. (generate emits graph before the chained
	// gqlgen pre-flight fails, so the config's relative ./graph is exercised.)
	t.Chdir(dir)
	writeAPIGraphQLConfig(t, cfgPath, sqlDir, outDir, "")
	writeSQLFile(t, sqlDir, "CREATE TABLE products (id uuid PRIMARY KEY, name text NOT NULL);")

	t.Setenv("SQLGEN_NO_GRAPHQL", "1")
	_, stderr, err := executeCommand("--config", cfgPath)
	if err != nil {
		if strings.Contains(stderr, "sqlgen graphql init") {
			t.Errorf("SQLGEN_NO_GRAPHQL must skip the chained gqlgen call; pre-flight error fired anyway:\n%s", stderr)
		}
	}
}

// TestPrintGenerateReport_FoldsGraphQLFiles pins the chained-graphql reporting
// integration: the chained gqlgen wrapper's emitted seed files must fold
// into both the verbose listing (`graphql seed: <path>` lines after the
// existing file list, before any `deleted:` lines) AND the headline file
// count (`generated %d files`) so the totals stay accurate when gqlgen
// runs inline as part of `sqlgen generate`.
func TestPrintGenerateReport_FoldsGraphQLFiles(t *testing.T) {
	schema := &parser.Schema{}
	result := &gen.GenerateResult{
		Files:      []string{"models/products_gen.go", "models/users_gen.go"},
		TableCount: 2,
		ViewCount:  0,
	}
	graphqlFiles := []string{"graph/resolver.go", "graph/products_gen.resolvers.go"}
	deletedFiles := []string{"models/legacy_gen.go"}

	t.Run("verbose lists graphql seeds + deletions", func(t *testing.T) {
		var buf bytes.Buffer
		flags := &cliFlags{verbose: true}
		printGenerateReport(&buf, flags, schema, result, deletedFiles, graphqlFiles, 12*time.Millisecond)
		out := buf.String()

		for _, want := range []string{
			"graphql seed: graph/resolver.go",
			"graphql seed: graph/products_gen.resolvers.go",
			"deleted: models/legacy_gen.go",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("verbose output missing %q\n--- output ---\n%s", want, out)
			}
		}

		// Layout invariant: graphql seeds come AFTER the verbose header
		// (which lists result.Files) and BEFORE the deleted lines, so a
		// reader scans `models/... → graphql seed: ... → deleted: ...`.
		seedIdx := strings.Index(out, "graphql seed:")
		delIdx := strings.Index(out, "deleted:")
		if seedIdx < 0 || delIdx < 0 || seedIdx >= delIdx {
			t.Errorf("expected `graphql seed:` lines before `deleted:` lines\n--- output ---\n%s", out)
		}
	})

	t.Run("non-verbose folds graphql files into headline count", func(t *testing.T) {
		var buf bytes.Buffer
		flags := &cliFlags{}
		printGenerateReport(&buf, flags, schema, result, nil, graphqlFiles, 33*time.Millisecond)
		out := buf.String()

		// 2 result files + 2 graphql files = 4 total.
		if !strings.Contains(out, "generated 4 files") {
			t.Errorf("headline count must include graphql files (want `generated 4 files`)\n--- output ---\n%s", out)
		}
		if strings.Contains(out, "generated 2 files") {
			t.Errorf("headline count looks like result.Files only — graphql files weren't folded in\n--- output ---\n%s", out)
		}
	})

	t.Run("non-verbose without graphql files matches result.Files count", func(t *testing.T) {
		var buf bytes.Buffer
		flags := &cliFlags{}
		printGenerateReport(&buf, flags, schema, result, nil, nil, 5*time.Millisecond)
		out := buf.String()
		if !strings.Contains(out, "generated 2 files") {
			t.Errorf("with no graphql files, headline = %q, want `generated 2 files`", out)
		}
	})

	t.Run("quiet suppresses everything", func(t *testing.T) {
		var buf bytes.Buffer
		flags := &cliFlags{quiet: true}
		printGenerateReport(&buf, flags, schema, result, deletedFiles, graphqlFiles, time.Millisecond)
		if buf.Len() != 0 {
			t.Errorf("quiet mode wrote %d bytes, want 0\n--- output ---\n%s", buf.Len(), buf.String())
		}
	})
}

// writeAPIGraphQLConfig writes a sqlgen.yml with api.graphql.enabled: true.
// The optional gqlgenConfigPath (empty == default ./gqlgen.yml) is forwarded
// into api.graphql.gqlgen_config so tests can point the chain at a temp file.
func writeAPIGraphQLConfig(t *testing.T, path, sqlDir, outputDir, gqlgenConfigPath string) {
	t.Helper()
	gqlgenConfigLine := ""
	if gqlgenConfigPath != "" {
		gqlgenConfigLine = "    gqlgen_config: " + gqlgenConfigPath + "\n"
	}
	content := []byte(`input:
  dialect: postgres
  paths:
    - "` + sqlDir + `"
output:
  driver: pgx
  dir: ` + outputDir + `
  package: models
  layout: file_per_table
api:
  enabled: true
  graphql:
    enabled: true
    schema_dir: ./graph
    resolver_dir: ./graph
    package: graph
    field_casing: camel_case
` + gqlgenConfigLine)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
}
