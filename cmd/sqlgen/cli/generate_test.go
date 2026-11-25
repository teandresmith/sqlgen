package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// TestResolveManifestTimestamp pins the --manifest-timestamp resolution: an
// explicit RFC3339 value is used verbatim (pinning generated_at for
// deterministic manifest output), an unset flag falls back to a valid RFC3339
// UTC timestamp, and anything else is a *usageError.
func TestResolveManifestTimestamp(t *testing.T) {
	const fixed = "2026-07-09T12:34:56Z"
	if got, err := resolveManifestTimestamp(fixed); err != nil || got != fixed {
		t.Errorf("resolveManifestTimestamp(%q) = %q, %v; want verbatim, nil", fixed, got, err)
	}

	got, err := resolveManifestTimestamp("")
	if err != nil {
		t.Fatalf("resolveManifestTimestamp(\"\") error: %v", err)
	}
	parsed, err := time.Parse(time.RFC3339, got)
	if err != nil {
		t.Fatalf("fallback timestamp %q is not RFC3339: %v", got, err)
	}
	if loc := parsed.Location(); loc != time.UTC {
		t.Errorf("fallback timestamp %q is not UTC (location %v)", got, loc)
	}

	_, err = resolveManifestTimestamp("2026-07-09")
	if _, ok := errors.AsType[*usageError](err); !ok {
		t.Errorf("resolveManifestTimestamp(%q) error = %v, want *usageError", "2026-07-09", err)
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

// TestGenerate_ChainedGraphQL_WrapperErrorLabel pins that only a failure of
// the gqlgen binary itself is reported as "gqlgen subprocess failed" (PRD
// §26.5.6); an error raised by the wrapper's own steps is labelled
// "gqlgen wrapper". Both keep the artifacts-emitted suffix.
func TestGenerate_ChainedGraphQL_WrapperErrorLabel(t *testing.T) {
	tests := []struct {
		name       string
		gqlgenYML  string
		gqlgenBin  string
		wantPrefix string
	}{
		{
			name:       "gqlgen binary fails",
			gqlgenYML:  "schema:\n  - graph/*.graphqls\nresolver:\n  layout: follow-schema\n  dir: graph\n  package: graph\n",
			gqlgenBin:  "no-such-gqlgen-binary",
			wantPrefix: "Error: gqlgen subprocess failed: running gqlgen (no-such-gqlgen-binary): ",
		},
		{
			name:       "single-file resolver block without filename",
			gqlgenYML:  "schema:\n  - graph/*.graphqls\nresolver:\n  layout: single-file\n  dir: graph\n  package: graph\n",
			gqlgenBin:  "no-such-gqlgen-binary",
			wantPrefix: "Error: gqlgen wrapper: parsing gqlgen.yml resolver block: resolver.filename is required with resolver.layout: single-file; ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "sqlgen.yml")
			sqlDir := filepath.Join(dir, "migrations")
			if err := os.MkdirAll(sqlDir, 0o750); err != nil {
				t.Fatalf("creating migrations dir: %v", err)
			}
			outDir := filepath.Join(dir, "output")
			gqlgenPath := filepath.Join(dir, "gqlgen.yml")
			if err := os.WriteFile(gqlgenPath, []byte(tt.gqlgenYML), 0o600); err != nil {
				t.Fatalf("writing gqlgen.yml: %v", err)
			}
			t.Chdir(dir)
			writeAPIGraphQLConfig(t, cfgPath, sqlDir, outDir, gqlgenPath)
			appendConfig(t, cfgPath, "    gqlgen_bin: "+tt.gqlgenBin+"\n")
			writeSQLFile(t, sqlDir, "CREATE TABLE products (id uuid PRIMARY KEY, name text NOT NULL);")

			_, stderr, err := executeCommand("--config", cfgPath)
			if err == nil {
				t.Fatalf("generate with a failing gqlgen wrapper returned nil error")
			}
			_, after, found := strings.Cut(stderr, "Error: ")
			if !found {
				t.Fatalf("generate stderr = %q, want an \"Error: \" line", stderr)
			}
			line := "Error: " + after
			if !strings.HasPrefix(line, tt.wantPrefix) {
				t.Errorf("generate stderr = %q, want a line starting %q", stderr, tt.wantPrefix)
			}
			wantSuffix := "; sqlgen-side artifacts emitted at " + outDir + "\n"
			if !strings.HasSuffix(line, wantSuffix) {
				t.Errorf("generate stderr = %q, want it to end %q", stderr, wantSuffix)
			}
		})
	}
}

// TestGenerate_ChainedGraphQL_MultiLineWrapperError pins that a wrapper error
// spanning several lines, here yaml.v3's unmarshal errors for a resolver
// block of the wrong shape, carries the "gqlgen wrapper" label on every line
// and ends with the artifacts-emitted suffix once.
func TestGenerate_ChainedGraphQL_MultiLineWrapperError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	outDir := filepath.Join(dir, "output")
	gqlgenPath := filepath.Join(dir, "gqlgen.yml")
	gqlgenYML := "schema:\n  - graph/*.graphqls\nresolver:\n  layout: [a]\n  dir: [b]\n"
	if err := os.WriteFile(gqlgenPath, []byte(gqlgenYML), 0o600); err != nil {
		t.Fatalf("writing gqlgen.yml: %v", err)
	}
	t.Chdir(dir)
	writeAPIGraphQLConfig(t, cfgPath, sqlDir, outDir, gqlgenPath)
	appendConfig(t, cfgPath, "    gqlgen_bin: no-such-gqlgen-binary\n")
	writeSQLFile(t, sqlDir, "CREATE TABLE products (id uuid PRIMARY KEY, name text NOT NULL);")

	_, stderr, err := executeCommand("--config", cfgPath)
	if err == nil {
		t.Fatalf("generate with a malformed gqlgen.yml resolver block returned nil error")
	}
	_, after, found := strings.Cut(stderr, "Error: ")
	if !found {
		t.Fatalf("generate stderr = %q, want an \"Error: \" line", stderr)
	}
	want := "Error: gqlgen wrapper: parsing gqlgen.yml resolver block: unmarshalling resolver block: yaml: unmarshal errors:\n" +
		"Error: gqlgen wrapper: line 4: cannot unmarshal !!seq into string\n" +
		"Error: gqlgen wrapper: line 5: cannot unmarshal !!seq into string; sqlgen-side artifacts emitted at " + outDir + "\n"
	if diff := cmp.Diff(want, "Error: "+after); diff != "" {
		t.Errorf("generate stderr (-want +got):\n%s", diff)
	}
}

// TestJoinedErrorsPrefixEveryLine pins that each error of a joined
// config-validation or generation error carries the phase prefix, not only
// the first, under generate and under the commands that share
// loadAndValidate.
func TestJoinedErrorsPrefixEveryLine(t *testing.T) {
	tests := []struct {
		name    string
		dialect string
		graphql bool
		schema  string
		extra   string
		args    []string
		want    []string
	}{
		{
			name:    "pre-parse pair under generate",
			dialect: "mysql",
			schema:  "CREATE TABLE a (id bigint PRIMARY KEY);",
			extra:   "generation:\n  manifest:\n    enabled: false\n    embed_in_client: true\n",
			want: []string{
				`Config validation error: output.driver: pgx is only compatible with input.dialect: postgres (got "mysql")`,
				"Config validation error: manifest.embed_in_client: cannot be true when manifest.enabled is false (PRD §30.2 / §4.13)",
			},
		},
		{
			name:    "pre-parse pair under lint",
			dialect: "mysql",
			schema:  "CREATE TABLE a (id bigint PRIMARY KEY);",
			extra:   "generation:\n  manifest:\n    enabled: false\n    embed_in_client: true\n",
			args:    []string{"lint"},
			want: []string{
				`Config validation error: output.driver: pgx is only compatible with input.dialect: postgres (got "mysql")`,
				"Config validation error: manifest.embed_in_client: cannot be true when manifest.enabled is false (PRD §30.2 / §4.13)",
			},
		},
		{
			name:    "post-parse cursor_keys pair under generate",
			dialect: "postgres",
			schema:  "CREATE TABLE a (id bigint PRIMARY KEY); CREATE TABLE b (id bigint PRIMARY KEY);",
			extra:   "tables:\n  a:\n    cursor_keys: [nope]\n  b:\n    cursor_keys: [nope]\n",
			want: []string{
				`Post-parse validation error: cursor_keys: column "nope" does not exist in table public.a`,
				`Post-parse validation error: cursor_keys: column "nope" does not exist in table public.b`,
			},
		},
		{
			name:    "post-parse cursor_keys pair under lint",
			dialect: "postgres",
			schema:  "CREATE TABLE a (id bigint PRIMARY KEY); CREATE TABLE b (id bigint PRIMARY KEY);",
			extra:   "tables:\n  a:\n    cursor_keys: [nope]\n  b:\n    cursor_keys: [nope]\n",
			args:    []string{"lint"},
			want: []string{
				`Post-parse validation error: cursor_keys: column "nope" does not exist in table public.a`,
				`Post-parse validation error: cursor_keys: column "nope" does not exist in table public.b`,
			},
		},
		{
			name:    "generation pair under generate",
			dialect: "postgres",
			graphql: true,
			schema:  "CREATE TABLE query (id uuid PRIMARY KEY); CREATE TABLE mutation (id uuid PRIMARY KEY);",
			args:    []string{"--no-graphql"},
			want: []string{
				`Generation error: building api context: api: table public.mutation resolves to the GraphQL type name "Mutation", which the generated shared_gen.graphqls already declares — set tables.public.mutation.struct_name to a different Go identifier (PRD §26.4)`,
				`Generation error: api: table public.query resolves to the GraphQL type name "Query", which the generated shared_gen.graphqls already declares — set tables.public.query.struct_name to a different Go identifier (PRD §26.4)`,
			},
		},
		{
			name:    "generation pair under diff",
			dialect: "postgres",
			graphql: true,
			schema:  "CREATE TABLE query (id uuid PRIMARY KEY); CREATE TABLE mutation (id uuid PRIMARY KEY);",
			args:    []string{"diff"},
			want: []string{
				`Generation error: building api context: api: table public.mutation resolves to the GraphQL type name "Mutation", which the generated shared_gen.graphqls already declares — set tables.public.mutation.struct_name to a different Go identifier (PRD §26.4)`,
				`Generation error: api: table public.query resolves to the GraphQL type name "Query", which the generated shared_gen.graphqls already declares — set tables.public.query.struct_name to a different Go identifier (PRD §26.4)`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "sqlgen.yml")
			sqlDir := filepath.Join(dir, "migrations")
			if err := os.MkdirAll(sqlDir, 0o750); err != nil {
				t.Fatalf("creating migrations dir: %v", err)
			}
			outDir := filepath.Join(dir, "output")
			t.Chdir(dir)
			if tt.graphql {
				writeAPIGraphQLConfig(t, cfgPath, sqlDir, outDir, "")
			} else {
				writeTestConfigWithLayout(t, cfgPath, tt.dialect, sqlDir, outDir, "file_per_table")
			}
			if tt.extra != "" {
				appendConfig(t, cfgPath, tt.extra)
			}
			writeSQLFile(t, sqlDir, tt.schema)

			args := append(slices.Clone(tt.args), "--config", cfgPath)
			_, stderr, err := executeCommand(args...)
			if err == nil {
				t.Fatalf("%v returned nil error", args)
			}
			got := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("%v stderr lines (-want +got):\n%s", args, diff)
			}
		})
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
