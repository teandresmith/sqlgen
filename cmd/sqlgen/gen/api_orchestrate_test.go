package gen_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
)

// TestAPIImportDir_PrefersOriginalOutputDir pins the API import path rule: it
// is computed from opts.OriginalOutputDir when set so external callers (e.g.
// the E2E golden harness rewriting output.dir to a per-test temp directory)
// can keep import paths stable while still redirecting the *write* location.
// Falls back to cfg.Output.Dir when OriginalOutputDir is unset so the default
// generation path (no redirection) is unaffected.
//
// The end-to-end import-path correctness — JoinModulePath against a real
// go.mod plus the rewriter env var — is exercised by TestE2EGoldenFiles,
// which fails on golden-diff if any API-side `_gen.go` file references the
// rewritten /var/folders/... path. This unit test pins just the fallback
// logic so a regression of the helper itself surfaces immediately.
func TestAPIImportDir_PrefersOriginalOutputDir(t *testing.T) {
	tests := []struct {
		name      string
		opts      gen.Options
		outputDir string
		want      string
	}{
		{
			name:      "no override returns cfg.Output.Dir",
			opts:      gen.Options{OutputDir: "./models"},
			outputDir: "./models",
			want:      "./models",
		},
		{
			name:      "override takes precedence over cfg.Output.Dir",
			opts:      gen.Options{OutputDir: "/tmp/foo", OriginalOutputDir: "./models"},
			outputDir: "/tmp/foo",
			want:      "./models",
		},
		{
			name:      "empty override falls back to cfg.Output.Dir",
			opts:      gen.Options{OutputDir: "/tmp/foo"},
			outputDir: "/tmp/foo",
			want:      "/tmp/foo",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.RootConfig{
				Output: config.OutputConfig{Dir: tc.outputDir},
			}
			got := gen.APIImportDirForTest(&tc.opts, cfg)
			if got != tc.want {
				t.Errorf("apiImportDir = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestResolveGraphDir pins §26.5.8 root-relative resolution for the graph
// write dir: relative dirs join onto the project root (NOT output.dir);
// absolute dirs pass through verbatim. The topology (nested vs top-level) is a
// pure function of the resolved path — there is no mode flag.
func TestResolveGraphDir(t *testing.T) {
	tests := []struct {
		name        string
		projectRoot string
		dir         string
		want        string
	}{
		{name: "nested default", projectRoot: ".", dir: "models/graph", want: "models/graph"},
		{name: "top-level sibling", projectRoot: ".", dir: "graph", want: "graph"},
		{name: "custom", projectRoot: ".", dir: "api/graph", want: "api/graph"},
		{name: "leading dot-slash cleaned", projectRoot: ".", dir: "./graph", want: "graph"},
		{name: "non-cwd project root", projectRoot: "/tmp/root", dir: "graph", want: "/tmp/root/graph"},
		{name: "absolute dir verbatim", projectRoot: ".", dir: "/abs/graph", want: "/abs/graph"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := gen.ResolveGraphDirForTest(tc.projectRoot, tc.dir); got != tc.want {
				t.Errorf("resolveGraphDir(%q, %q) = %q, want %q", tc.projectRoot, tc.dir, got, tc.want)
			}
		})
	}
}

// TestGraphImportPaths_RootRelative pins §26.5.8: the
// graph package import path joins resolver_dir onto the module directly, with
// no output.dir prefix. The decisive case sets output.dir=models but
// resolver_dir=graph (top-level) and expects <module>/graph — NOT
// <module>/models/graph, which the retired output.dir-relative join produced.
// ModelsImportPath still tracks output.dir, so the two paths diverge exactly
// when graph lives outside the models tree.
func TestGraphImportPaths_RootRelative(t *testing.T) {
	tests := []struct {
		name             string
		outputDir        string
		resolverDir      string
		wantResolver     string
		wantSqlgenHelper string
	}{
		{
			name:             "nested default",
			outputDir:        "models",
			resolverDir:      "models/graph",
			wantResolver:     "example.com/foo/models/graph",
			wantSqlgenHelper: "example.com/foo/models/graph/sqlgenresolver",
		},
		{
			name:             "top-level sibling",
			outputDir:        "models",
			resolverDir:      "graph",
			wantResolver:     "example.com/foo/graph",
			wantSqlgenHelper: "example.com/foo/graph/sqlgenresolver",
		},
		{
			name:             "custom",
			outputDir:        "models",
			resolverDir:      "api/graph",
			wantResolver:     "example.com/foo/api/graph",
			wantSqlgenHelper: "example.com/foo/api/graph/sqlgenresolver",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			wd, err := os.Getwd()
			if err != nil {
				t.Fatalf("getwd: %v", err)
			}
			t.Cleanup(func() { _ = os.Chdir(wd) })
			if err := os.Chdir(dir); err != nil {
				t.Fatalf("chdir: %v", err)
			}
			if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/foo\n\ngo 1.26\n"), 0o600); err != nil {
				t.Fatalf("writing go.mod: %v", err)
			}

			cfg := &config.RootConfig{
				Output: config.OutputConfig{
					Package: "models",
					Dir:     tt.outputDir,
					Client:  &config.ClientOutputConfig{Name: "Client"},
				},
				API: &config.APIConfig{
					Enabled: true,
					GraphQL: &config.GraphQLAPIConfig{
						Enabled:     true,
						ResolverDir: tt.resolverDir,
					},
				},
			}
			apiCtx := &gen.APIContext{}
			gen.PopulateAPIContextResolverFields(apiCtx, cfg, tt.outputDir)

			if apiCtx.ResolverImportPath != tt.wantResolver {
				t.Errorf("ResolverImportPath = %q, want %q", apiCtx.ResolverImportPath, tt.wantResolver)
			}
			if apiCtx.SqlgenResolverImportPath != tt.wantSqlgenHelper {
				t.Errorf("SqlgenResolverImportPath = %q, want %q", apiCtx.SqlgenResolverImportPath, tt.wantSqlgenHelper)
			}
			if want := "example.com/foo/" + tt.outputDir; apiCtx.ModelsImportPath != want {
				t.Errorf("ModelsImportPath = %q, want %q", apiCtx.ModelsImportPath, want)
			}
		})
	}
}

// TestBuildAPIContext_DisabledIsInert pins §26.5.8's disabled-API rule at the
// emission gate: with api.graphql.enabled false, BuildAPIContext returns a nil
// context even when schema_dir / resolver_dir are set, so generateAPI emits
// nothing and no graph dir is resolved.
func TestBuildAPIContext_DisabledIsInert(t *testing.T) {
	cfg := &config.RootConfig{
		Output: config.OutputConfig{Package: "models", Dir: "models"},
		API: &config.APIConfig{
			Enabled: true,
			GraphQL: &config.GraphQLAPIConfig{
				Enabled:     false,
				SchemaDir:   "graph",
				ResolverDir: "graph",
			},
		},
	}
	apiCtx, err := gen.BuildAPIContext(nil, nil, nil, nil, cfg)
	if err != nil {
		t.Fatalf("BuildAPIContext: unexpected error: %v", err)
	}
	if apiCtx != nil {
		t.Errorf("BuildAPIContext: expected nil context when api.graphql disabled, got %+v", apiCtx)
	}
}
