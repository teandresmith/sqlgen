package wrapper

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestMain doubles as a stub gqlgen binary. When the helper env var is set
// the test process immediately exits with the requested status, which lets
// the gen tests exercise the success and failure paths without a real gqlgen
// install. Otherwise the test suite runs normally.
func TestMain(m *testing.M) {
	switch os.Getenv("SQLGEN_WRAPPER_TEST_HELPER") {
	case "succeed":
		os.Exit(0)
	case "fail":
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func TestInit_GreenfieldScaffold(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gqlgen.yml")

	if err := Init(path); err != nil {
		t.Fatalf("Init: %v", err)
	}
	data, err := os.ReadFile(path) //nolint:gosec // path is t.TempDir-derived
	if err != nil {
		t.Fatalf("reading scaffolded file: %v", err)
	}
	if !strings.Contains(string(data), `"graph/*.graphqls"`) {
		t.Errorf("scaffold missing schema glob: %s", data)
	}
}

func TestInit_NoOverwriteOnExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gqlgen.yml")

	original := []byte("schema:\n  - graph/custom.graphqls\n# consumer-owned\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("seeding file: %v", err)
	}

	if err := Init(path); err != nil {
		t.Fatalf("Init returned an error on existing file: %v", err)
	}

	got, err := os.ReadFile(path) //nolint:gosec // path is t.TempDir-derived
	if err != nil {
		t.Fatalf("reading file post-Init: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Errorf("Init overwrote existing file:\n--- want ---\n%s\n--- got ---\n%s", original, got)
	}
}

func TestGen_TempFileCleanupOnSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper invocation pattern is POSIX-only")
	}

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gqlgen.yml")
	if err := os.WriteFile(cfgPath, []byte("schema:\n  - graph/custom.graphqls\n"), 0o600); err != nil {
		t.Fatalf("seeding gqlgen.yml: %v", err)
	}

	tempDir := isolateTempDir(t)
	t.Setenv("SQLGEN_WRAPPER_TEST_HELPER", "succeed")

	_, err := Gen(context.Background(), GenOptions{
		GqlgenConfigPath: cfgPath,
		GqlgenBin:        os.Args[0],
		MergeInput:       MergeInput{SchemaGlob: "graph/*.graphqls"},
		Stdout:           &bytes.Buffer{},
		Stderr:           &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("Gen on success path: %v", err)
	}

	assertNoLeakedTempFiles(t, tempDir)
}

func TestGen_TempFileCleanupOnSubprocessFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper invocation pattern is POSIX-only")
	}

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gqlgen.yml")
	if err := os.WriteFile(cfgPath, []byte("schema:\n  - graph/custom.graphqls\n"), 0o600); err != nil {
		t.Fatalf("seeding gqlgen.yml: %v", err)
	}

	tempDir := isolateTempDir(t)
	t.Setenv("SQLGEN_WRAPPER_TEST_HELPER", "fail")

	_, err := Gen(context.Background(), GenOptions{
		GqlgenConfigPath: cfgPath,
		GqlgenBin:        os.Args[0],
		MergeInput:       MergeInput{SchemaGlob: "graph/*.graphqls"},
		Stdout:           &bytes.Buffer{},
		Stderr:           &bytes.Buffer{},
	})
	if err == nil {
		t.Fatalf("Gen on failure path: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "running gqlgen") {
		t.Errorf("error %q is missing the sqlgen-flavoured wrap", err)
	}

	assertNoLeakedTempFiles(t, tempDir)
}

func TestGen_RejectsMissingConfigPath(t *testing.T) {
	_, err := Gen(context.Background(), GenOptions{
		GqlgenConfigPath: "",
		GqlgenBin:        "go run example.com/x",
	})
	if err == nil {
		t.Fatalf("expected error for empty config path")
	}
}

func TestGen_RejectsEmptyGqlgenBin(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gqlgen.yml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("seeding gqlgen.yml: %v", err)
	}
	_, err := Gen(context.Background(), GenOptions{
		GqlgenConfigPath: cfgPath,
		GqlgenBin:        "",
	})
	if err == nil {
		t.Fatalf("expected error for empty gqlgen_bin")
	}
}

func TestGen_MalformedConfigErrorPath(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gqlgen.yml")
	if err := os.WriteFile(cfgPath, []byte("schema:\n\t- bad\n"), 0o600); err != nil {
		t.Fatalf("seeding gqlgen.yml: %v", err)
	}
	t.Setenv("SQLGEN_WRAPPER_TEST_HELPER", "succeed")

	_, err := Gen(context.Background(), GenOptions{
		GqlgenConfigPath: cfgPath,
		GqlgenBin:        os.Args[0],
	})
	if err == nil {
		t.Fatalf("expected error for malformed yaml, got nil")
	}
	// Parse-layout runs before merge so the layout can
	// drive the merged config's `skip_validation` override. A malformed
	// YAML body fails the resolver-block parse first.
	if !strings.Contains(err.Error(), "merging gqlgen config") &&
		!strings.Contains(err.Error(), "parsing gqlgen.yml resolver block") {
		t.Errorf("error %q is missing a recognisable YAML-parse prefix", err)
	}
}

// TestSubprocessEnv pins that when the gqlgen invocation is `go run …`,
// the subprocess env must include GOFLAGS=-mod=mod so transitive build deps
// resolve on demand. For any other invocation form, the parent env is
// inherited unchanged.
func TestSubprocessEnv(t *testing.T) {
	tests := []struct {
		name     string
		binName  string
		binArgs  []string
		wantFlag bool
	}{
		{name: "go run package", binName: "go", binArgs: []string{"run", "github.com/99designs/gqlgen"}, wantFlag: true},
		{name: "go run multi-arg", binName: "go", binArgs: []string{"run", "./tools/gqlgen", "--verbose"}, wantFlag: true},
		{name: "go build is not go run", binName: "go", binArgs: []string{"build", "-o", "gqlgen"}, wantFlag: false},
		{name: "go with no args", binName: "go", binArgs: nil, wantFlag: false},
		{name: "non-go binary", binName: "gqlgen", binArgs: nil, wantFlag: false},
		{name: "non-go binary with run-shaped args", binName: "gqlgen", binArgs: []string{"run", "x"}, wantFlag: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := subprocessEnv(tt.binName, tt.binArgs)
			gotFlag := containsEnv(env, "GOFLAGS=-mod=mod")
			if gotFlag != tt.wantFlag {
				t.Errorf("subprocessEnv(%q, %v) GOFLAGS=-mod=mod present: got %v, want %v",
					tt.binName, tt.binArgs, gotFlag, tt.wantFlag)
			}
			// Always inherits parent env: PATH should always be present (set on
			// every standard CI/dev environment).
			if !containsEnvPrefix(env, "PATH=") {
				t.Errorf("subprocessEnv(%q, %v) did not inherit PATH from parent env",
					tt.binName, tt.binArgs)
			}
		})
	}
}

// TestGen_GoRunInheritsModMode is the integration regression for GOFLAGS
// inheritance. It
// stages a tiny self-contained Go program in a temp dir, invokes wrapper.Gen
// with GqlgenBin = "go run .", and verifies that the subprocess sees
// GOFLAGS=-mod=mod in its environment. The staged program prints its
// GOFLAGS env var to stdout and exits 0; it does not depend on any external
// modules, so the test runs offline.
func TestGen_GoRunInheritsModMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test invocation pattern is POSIX-only")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module gorunmodtest\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatalf("seeding go.mod: %v", err)
	}
	mainSrc := `package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println("GOFLAGS=" + os.Getenv("GOFLAGS"))
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainSrc), 0o600); err != nil {
		t.Fatalf("seeding main.go: %v", err)
	}

	cfgPath := filepath.Join(dir, "gqlgen.yml")
	if err := os.WriteFile(cfgPath, []byte("schema:\n  - graph/custom.graphqls\n"), 0o600); err != nil {
		t.Fatalf("seeding gqlgen.yml: %v", err)
	}

	var stdout, stderr bytes.Buffer
	_, err := Gen(context.Background(), GenOptions{
		GqlgenConfigPath: cfgPath,
		GqlgenBin:        "go run .",
		WorkingDir:       dir,
		MergeInput:       MergeInput{SchemaGlob: "graph/*.graphqls"},
		Stdout:           &stdout,
		Stderr:           &stderr,
	})
	if err != nil {
		t.Fatalf("Gen with go run: %v\nstderr: %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "GOFLAGS=-mod=mod") {
		t.Errorf("subprocess did not see GOFLAGS=-mod=mod\nstdout: %s\nstderr: %s",
			stdout.String(), stderr.String())
	}
}

func TestSplitBin(t *testing.T) {
	tests := []struct {
		in       string
		wantName string
		wantArgs []string
		wantErr  bool
	}{
		{in: "gqlgen", wantName: "gqlgen", wantArgs: nil},
		{in: "go run github.com/99designs/gqlgen", wantName: "go", wantArgs: []string{"run", "github.com/99designs/gqlgen"}},
		{in: "  gqlgen  ", wantName: "gqlgen", wantArgs: nil},
		{in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			name, args, err := splitBin(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for input %q", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("splitBin: %v", err)
			}
			if name != tt.wantName {
				t.Errorf("name: got %q, want %q", name, tt.wantName)
			}
			if !slices.Equal(args, tt.wantArgs) {
				t.Errorf("args: got %v, want %v", args, tt.wantArgs)
			}
		})
	}
}

// isolateTempDir points os.TempDir() at a t.TempDir so post-run leak checks
// only consider files this test could have produced.
func isolateTempDir(t *testing.T) string {
	t.Helper()
	tempDir := t.TempDir()
	t.Setenv("TMPDIR", tempDir)
	return tempDir
}

// assertNoLeakedTempFiles verifies that no sqlgen-gqlgen-*.yml files remain
// in the isolated temp dir after Gen returns.
func assertNoLeakedTempFiles(t *testing.T, tempDir string) {
	t.Helper()
	var leaks []string
	err := filepath.WalkDir(tempDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if strings.HasPrefix(base, "sqlgen-gqlgen-") && strings.HasSuffix(base, ".yml") {
			leaks = append(leaks, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking temp dir: %v", err)
	}
	if len(leaks) > 0 {
		t.Errorf("leaked %d temp files: %v", len(leaks), leaks)
	}
}

// containsEnv reports whether the env slice contains the exact key=value
// entry. Used by TestSubprocessEnv to assert GOFLAGS injection.
func containsEnv(env []string, want string) bool {
	return slices.Contains(env, want)
}

// containsEnvPrefix reports whether any env entry starts with prefix. Used to
// confirm that the parent's environment is inherited (e.g. PATH=).
func containsEnvPrefix(env []string, prefix string) bool {
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return true
		}
	}
	return false
}

// TestParseModelLayout pins the consumer's gqlgen.yml model block extraction.
// Defaults must fall through to gqlgen's documented
// "graph/model/models_gen.go" / "model" shape when keys are absent.
func TestParseModelLayout(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		wantFile string
		wantPkg  string
	}{
		{
			name:     "all defaults",
			yaml:     "schema:\n  - graph/*.graphqls\n",
			wantFile: defaultModelFilename,
			wantPkg:  defaultModelPackage,
		},
		{
			name:     "explicit defaults",
			yaml:     "model:\n  filename: graph/model/models_gen.go\n  package: model\n",
			wantFile: "graph/model/models_gen.go",
			wantPkg:  "model",
		},
		{
			name:     "custom path and package",
			yaml:     "model:\n  filename: api/types/types_gen.go\n  package: types\n",
			wantFile: "api/types/types_gen.go",
			wantPkg:  "types",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseModelLayout([]byte(tt.yaml))
			if err != nil {
				t.Fatalf("parseModelLayout: %v", err)
			}
			if got.Filename != tt.wantFile {
				t.Errorf("Filename: got %q, want %q", got.Filename, tt.wantFile)
			}
			if got.Package != tt.wantPkg {
				t.Errorf("Package: got %q, want %q", got.Package, tt.wantPkg)
			}
		})
	}
}

// TestLoadGqlgenModelInfo pins the public helper that resolves the gqlgen
// model package's import path from the consumer's gqlgen.yml + module path
// Empty inputs and missing files yield zero-value GqlgenModelInfo
// (no error), matching the fail-soft path used elsewhere when go.mod /
// gqlgen.yml are unresolvable.
func TestLoadGqlgenModelInfo(t *testing.T) {
	t.Run("empty inputs return zero value", func(t *testing.T) {
		info, err := LoadGqlgenModelInfo("", "")
		if err != nil {
			t.Fatalf("LoadGqlgenModelInfo: %v", err)
		}
		if info != (GqlgenModelInfo{}) {
			t.Errorf("expected zero-value GqlgenModelInfo, got %+v", info)
		}
	})

	t.Run("missing file returns zero value", func(t *testing.T) {
		dir := t.TempDir()
		info, err := LoadGqlgenModelInfo(filepath.Join(dir, "absent.yml"), "example.com/foo")
		if err != nil {
			t.Fatalf("LoadGqlgenModelInfo: %v", err)
		}
		if info != (GqlgenModelInfo{}) {
			t.Errorf("expected zero-value GqlgenModelInfo for missing file, got %+v", info)
		}
	})

	t.Run("default model layout joins with module path", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "gqlgen.yml")
		if err := os.WriteFile(path, []byte("schema:\n  - graph/*.graphqls\n"), 0o600); err != nil {
			t.Fatalf("writing gqlgen.yml: %v", err)
		}
		info, err := LoadGqlgenModelInfo(path, "example.com/foo")
		if err != nil {
			t.Fatalf("LoadGqlgenModelInfo: %v", err)
		}
		want := GqlgenModelInfo{
			ImportPath: "example.com/foo/graph/model",
			Alias:      GqlgenModelImportAlias,
		}
		if info != want {
			t.Errorf("expected %+v, got %+v", want, info)
		}
	})

	t.Run("custom model.filename produces matching import path", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "gqlgen.yml")
		if err := os.WriteFile(path, []byte("model:\n  filename: api/types/types_gen.go\n  package: types\n"), 0o600); err != nil {
			t.Fatalf("writing gqlgen.yml: %v", err)
		}
		info, err := LoadGqlgenModelInfo(path, "example.com/foo")
		if err != nil {
			t.Fatalf("LoadGqlgenModelInfo: %v", err)
		}
		want := GqlgenModelInfo{
			ImportPath: "example.com/foo/api/types",
			Alias:      GqlgenModelImportAlias,
		}
		if info != want {
			t.Errorf("expected %+v, got %+v", want, info)
		}
	})
}

// TestBuildSeedFileContents_GqlgenModelImport pins the seed file
// preamble: when SeedImports carries a non-empty GqlgenModelImportPath +
// GqlgenModelAlias, the rendered preamble must include the matching
// aliased import line so seed bodies referencing
// `<alias>.Create<T>Input` resolve. With the gqlgen-model fields empty the
// import line is dropped, preserving the legacy preamble shape.
func TestBuildSeedFileContents_GqlgenModelImport(t *testing.T) {
	body := "func (r *queryResolver) Product(ctx context.Context, id string) (*models.Product, error) {\n\treturn r.Q.Product(ctx, id)\n}\n"

	t.Run("emits aliased import when set", func(t *testing.T) {
		out := buildSeedFileContents("graph", SeedImports{
			ModelsAlias:              "models",
			ModelsImportPath:         "example.com/foo/gen",
			SqlgenResolverImportPath: "example.com/foo/gen/graph/sqlgenresolver",
			GqlgenModelImportPath:    "example.com/foo/graph/model",
			GqlgenModelAlias:         GqlgenModelImportAlias,
		}, body)
		if !strings.Contains(out, `gqlmodel "example.com/foo/graph/model"`) {
			t.Errorf("expected aliased gqlmodel import in seed preamble:\n%s", out)
		}
		if !strings.Contains(out, `models "example.com/foo/gen"`) {
			t.Errorf("expected models import preserved:\n%s", out)
		}
		if !strings.Contains(out, `"example.com/foo/gen/graph/sqlgenresolver"`) {
			t.Errorf("expected sqlgenresolver import preserved:\n%s", out)
		}
	})

	t.Run("omits import when alias empty", func(t *testing.T) {
		out := buildSeedFileContents("graph", SeedImports{
			ModelsAlias:              "models",
			ModelsImportPath:         "example.com/foo/gen",
			SqlgenResolverImportPath: "example.com/foo/gen/graph/sqlgenresolver",
		}, body)
		if strings.Contains(out, `gqlmodel`) {
			t.Errorf("expected no gqlmodel import line when alias unset:\n%s", out)
		}
	})

	t.Run("omits import when only path set", func(t *testing.T) {
		// Defensive: both fields must be set together; either-or yields no
		// import line so an alias without a path can't accidentally produce
		// a syntactically-broken preamble.
		out := buildSeedFileContents("graph", SeedImports{
			ModelsAlias:           "models",
			ModelsImportPath:      "example.com/foo/gen",
			GqlgenModelImportPath: "example.com/foo/graph/model",
		}, body)
		if strings.Contains(out, `"example.com/foo/graph/model"`) {
			t.Errorf("expected no gqlmodel import line when alias unset:\n%s", out)
		}
	})
}

// TestParseResolverLayout pins the consumer's gqlgen.yml resolver block
// extraction. Defaults must fall through to gqlgen's documented
// follow-schema / `{name}.resolvers.go` shape when keys are absent.
func TestParseResolverLayout(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		wantLay  string
		wantDir  string
		wantTpl  string
		wantFile string
	}{
		{
			name:     "all defaults",
			yaml:     "schema:\n  - graph/*.graphqls\n",
			wantLay:  "follow-schema",
			wantDir:  "graph",
			wantTpl:  "{name}.resolvers.go",
			wantFile: "resolver.go",
		},
		{
			name:     "follow-schema explicit",
			yaml:     "resolver:\n  layout: follow-schema\n  dir: graph\n  package: graph\n  filename_template: \"{name}.resolvers.go\"\n",
			wantLay:  "follow-schema",
			wantDir:  "graph",
			wantTpl:  "{name}.resolvers.go",
			wantFile: "resolver.go",
		},
		{
			name:     "single-file with custom dir",
			yaml:     "resolver:\n  layout: single-file\n  dir: api\n  package: api\n  filename: combined.go\n",
			wantLay:  "single-file",
			wantDir:  "api",
			wantTpl:  "{name}.resolvers.go",
			wantFile: "combined.go",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseResolverLayout([]byte(tt.yaml))
			if err != nil {
				t.Fatalf("parseResolverLayout: %v", err)
			}
			if got.Layout != tt.wantLay {
				t.Errorf("Layout: got %q, want %q", got.Layout, tt.wantLay)
			}
			if got.Dir != tt.wantDir {
				t.Errorf("Dir: got %q, want %q", got.Dir, tt.wantDir)
			}
			if got.FilenameTemplate != tt.wantTpl {
				t.Errorf("FilenameTemplate: got %q, want %q", got.FilenameTemplate, tt.wantTpl)
			}
			if got.Filename != tt.wantFile {
				t.Errorf("Filename: got %q, want %q", got.Filename, tt.wantFile)
			}
		})
	}
}

// TestWriteSeedFiles_FollowSchemaOnlyWhenAbsent pins the seed writer's
// "never overwrite" invariant: the wrapper writes a seed file the first
// time but leaves any pre-existing file untouched on subsequent runs (gqlgen
// owns the file after the first run).
func TestWriteSeedFiles_FollowSchemaOnlyWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	resolverDir := filepath.Join(dir, "graph")
	if err := os.MkdirAll(resolverDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	opts := GenOptions{
		WorkingDir:  dir,
		SeedPackage: "graph",
		SeedImports: SeedImports{
			ModelsAlias:              "models",
			ModelsImportPath:         "example.com/foo/gen",
			SqlgenResolverImportPath: "example.com/foo/gen/graph/sqlgenresolver",
		},
		Seeds: []TableSeed{
			{SnakeName: "products", Body: "func (r *queryResolver) Product(ctx context.Context, id string) (*models.Product, error) { return r.Q.Product(ctx, id) }\n"},
		},
	}
	layout := resolverLayout{
		Layout: layoutFollowSchema, Dir: "graph", FilenameTemplate: defaultFilenameTpl, Filename: defaultSingleFilename,
	}

	if _, err := writeSeedFiles(opts, layout); err != nil {
		t.Fatalf("writeSeedFiles first run: %v", err)
	}

	expected := filepath.Join(resolverDir, "products_gen.resolvers.go")
	first, err := os.ReadFile(expected) //nolint:gosec // path is t.TempDir-derived
	if err != nil {
		t.Fatalf("reading seed file: %v", err)
	}
	if !strings.Contains(string(first), "r.Q.Product") {
		t.Errorf("seed file missing delegation: %s", first)
	}

	// Simulate a consumer edit + gqlgen ownership: change the body.
	consumerEdit := []byte("package graph\n\n// consumer-owned content\n")
	if err := os.WriteFile(expected, consumerEdit, 0o600); err != nil {
		t.Fatalf("seeding consumer edit: %v", err)
	}

	if _, err := writeSeedFiles(opts, layout); err != nil {
		t.Fatalf("writeSeedFiles second run: %v", err)
	}

	got, err := os.ReadFile(expected) //nolint:gosec // path is t.TempDir-derived
	if err != nil {
		t.Fatalf("reading seed file: %v", err)
	}
	if !bytes.Equal(got, consumerEdit) {
		t.Errorf("second run overwrote consumer-owned seed file:\nwant: %s\ngot:  %s", consumerEdit, got)
	}
}

// TestWriteSeedFiles_SingleFile pins the single-file layout: every table's
// body is concatenated into one file at <resolver.filename>.
func TestWriteSeedFiles_SingleFile(t *testing.T) {
	dir := t.TempDir()

	opts := GenOptions{
		WorkingDir:  dir,
		SeedPackage: "graph",
		SeedImports: SeedImports{
			ModelsAlias:              "models",
			ModelsImportPath:         "example.com/foo/gen",
			SqlgenResolverImportPath: "example.com/foo/gen/graph/sqlgenresolver",
		},
		Seeds: []TableSeed{
			{SnakeName: "products", Body: "// products body\n"},
			{SnakeName: "users", Body: "// users body\n"},
		},
	}
	layout := resolverLayout{
		Layout: layoutSingleFile, Dir: "graph", FilenameTemplate: defaultFilenameTpl, Filename: "graph/resolver.go",
	}

	if _, err := writeSeedFiles(opts, layout); err != nil {
		t.Fatalf("writeSeedFiles: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "graph", "resolver.go")) //nolint:gosec // path is t.TempDir-derived
	if err != nil {
		t.Fatalf("reading combined seed: %v", err)
	}
	if !strings.Contains(string(got), "// products body") || !strings.Contains(string(got), "// users body") {
		t.Errorf("combined seed missing per-table bodies: %s", got)
	}
}

// TestSingleFileLayout_FilenameIgnoresDir pins resolver-file placement. Under
// single-file gqlgen resolves resolver.filename against its working directory
// and ignores resolver.dir, so every step that touches the resolver file must
// find it at the same path gqlgen writes: the seed write, the post-gqlgen
// struct merge, and the stub pass's file list. Joining dir and filename
// broke the common `dir: graph` + `filename: graph/resolver.go` config.
func TestSingleFileLayout_FilenameIgnoresDir(t *testing.T) {
	tests := []struct {
		name     string
		dir      string
		filename string
		want     string // relative to the working directory
	}{
		{
			name:     "dir and filename name the same directory",
			dir:      "models/graph",
			filename: "models/graph/resolver.go",
			want:     "models/graph/resolver.go",
		},
		{
			name:     "dir names another directory",
			dir:      "api",
			filename: "combined.go",
			want:     "combined.go",
		},
		{
			name:     "dot dir",
			dir:      ".",
			filename: "models/graph/resolver.go",
			want:     "models/graph/resolver.go",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wd := t.TempDir()
			want := filepath.Join(wd, tt.want)
			opts := GenOptions{
				WorkingDir:  wd,
				SeedPackage: "graph",
				SeedImports: SeedImports{
					ModelsAlias:              "models",
					ModelsImportPath:         "example.com/foo/gen",
					SqlgenResolverImportPath: "example.com/foo/gen/graph/sqlgenresolver",
				},
				Seeds:               []TableSeed{{SnakeName: "products", Body: "// products body\n"}},
				SqlgenManagedFields: SqlgenManagedFieldSet{QueryFields: []string{"Product"}},
			}
			layout := resolverLayout{
				Layout: layoutSingleFile, Dir: tt.dir, FilenameTemplate: defaultFilenameTpl, Filename: tt.filename,
			}

			seeded, err := writeSeedFiles(opts, layout)
			if err != nil {
				t.Fatalf("writeSeedFiles: %v", err)
			}
			if diff := cmp.Diff([]string{want}, seeded); diff != "" {
				t.Fatalf("writeSeedFiles paths (-want +got):\n%s", diff)
			}

			// gqlgen rewrites the file and wipes the seeded struct fields.
			// The models import is declared so the merge's goimports pass
			// does not scan the module cache for it.
			wiped := "package graph\n\nimport models \"example.com/foo/gen\"\n\nvar _ *models.Client\n\ntype Resolver struct{}\n"
			if err := os.WriteFile(want, []byte(wiped), 0o600); err != nil {
				t.Fatalf("writing wiped resolver: %v", err)
			}
			merged, err := mergeSingleFileResolver(opts, layout)
			if err != nil {
				t.Fatalf("mergeSingleFileResolver: %v", err)
			}
			if merged != want {
				t.Errorf("mergeSingleFileResolver = %q, want %q", merged, want)
			}

			owned, err := gqlgenOwnedFiles(wd, layout)
			if err != nil {
				t.Fatalf("gqlgenOwnedFiles: %v", err)
			}
			if diff := cmp.Diff([]string{want}, owned); diff != "" {
				t.Errorf("gqlgenOwnedFiles (-want +got):\n%s", diff)
			}
		})
	}
}

// TestWriteSeedFiles_FollowSchema_WritesSlimResolverScaffold pins that under
// follow-schema layout, the wrapper pre-writes a
// SLIM `resolver.go` containing only `type Resolver struct { Client; Q; M }`
// — no `Query()` / `Mutation()` interface methods, no `queryResolver` /
// `mutationResolver` type defs. gqlgen v0.17.x emits those into the
// schema-source resolver file (typically `shared_gen.resolvers.go` for the
// shared schema that declares the root Query / Mutation types); emitting
// them here too would collide. Pre-populating the struct fields up front
// is REQUIRED so the per-table seeds (which reference `r.Q.<Field>` /
// `r.M.<Field>`) typecheck during gqlgen's pre-return validation pass.
func TestWriteSeedFiles_FollowSchema_WritesSlimResolverScaffold(t *testing.T) {
	dir := t.TempDir()
	resolverDir := filepath.Join(dir, "graph")
	if err := os.MkdirAll(resolverDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	opts := GenOptions{
		WorkingDir:  dir,
		SeedPackage: "graph",
		SeedImports: SeedImports{
			ModelsAlias:              "models",
			ModelsImportPath:         "example.com/foo/gen",
			SqlgenResolverImportPath: "example.com/foo/gen/graph/sqlgenresolver",
		},
		Seeds: []TableSeed{
			{SnakeName: "products", Body: "// products body\n"},
		},
		SqlgenManagedFields: SqlgenManagedFieldSet{
			QueryFields:    []string{"Product"},
			MutationFields: []string{"CreateProduct"},
		},
	}
	layout := resolverLayout{
		Layout: layoutFollowSchema, Dir: "graph", FilenameTemplate: defaultFilenameTpl, Filename: defaultSingleFilename,
	}

	written, err := writeSeedFiles(opts, layout)
	if err != nil {
		t.Fatalf("writeSeedFiles: %v", err)
	}

	resolverPath := filepath.Join(resolverDir, "resolver.go")
	got, err := os.ReadFile(resolverPath) //nolint:gosec // path is t.TempDir-derived
	if err != nil {
		t.Fatalf("reading scaffold: %v", err)
	}
	gotStr := string(got)
	flat := strings.Join(strings.Fields(gotStr), " ")

	wantPresent := []string{
		"package graph",
		"type Resolver struct {",
		"Client *models.Client",
		"Q *sqlgenresolver.Q",
		"M *sqlgenresolver.M",
	}
	for _, sub := range wantPresent {
		if !strings.Contains(flat, sub) {
			t.Errorf("scaffold missing %q\n--- output ---\n%s", sub, gotStr)
		}
	}

	// Slim-scaffold guarantee: gqlgen-owned shapes must NOT appear in our
	// pre-emitted resolver.go (they'd collide with gqlgen's emission to
	// shared_gen.resolvers.go).
	wantAbsent := []string{
		"func (r *Resolver) Query()",
		"func (r *Resolver) Mutation()",
		"type queryResolver",
		"type mutationResolver",
	}
	for _, sub := range wantAbsent {
		if strings.Contains(gotStr, sub) {
			t.Errorf("slim scaffold leaked gqlgen-owned shape %q\n--- output ---\n%s", sub, gotStr)
		}
	}

	// resolver.go appears in the written-paths report so the chained
	// `sqlgen generate` headline file count picks it up.
	foundResolver := false
	for _, p := range written {
		if filepath.Base(p) == "resolver.go" {
			foundResolver = true
		}
	}
	if !foundResolver {
		t.Errorf("written paths missing resolver.go (got %v)", written)
	}

	// Per-table seed file is also emitted.
	seedPath := filepath.Join(resolverDir, "products_gen.resolvers.go")
	if _, err := os.Stat(seedPath); err != nil {
		t.Errorf("expected seed %q to be written: %v", seedPath, err)
	}
}

// TestWriteSeedFiles_FreshScaffold_ImportsGoimportsSorted pins that the
// fresh-scaffold branch of writeResolverScaffold must sort its hand-built
// import block with goimports so it is byte-identical to the merge path (which
// runs imports.Process). Uses a TOP-LEVEL resolver dir (`graph`, a sibling of
// the models tree) — the layout where the fixed emission order (models, then
// sqlgenresolver) diverges from goimports' alphabetical sort: `graph/…/
// sqlgenresolver` sorts before `…/models` (`g` < `m`). Without the sort the
// scaffold emits models first and generation is non-idempotent.
func TestWriteSeedFiles_FreshScaffold_ImportsGoimportsSorted(t *testing.T) {
	dir := t.TempDir()
	resolverDir := filepath.Join(dir, "graph")
	if err := os.MkdirAll(resolverDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	opts := GenOptions{
		WorkingDir:  dir,
		SeedPackage: "graph",
		SeedImports: SeedImports{
			ModelsAlias:      "models",
			ModelsImportPath: "example.com/foo/models",
			// Top-level (sibling) resolver dir: NOT nested under models/.
			SqlgenResolverImportPath: "example.com/foo/graph/sqlgenresolver",
		},
		Seeds: []TableSeed{
			{SnakeName: "products", Body: "// products body\n"},
		},
		SqlgenManagedFields: SqlgenManagedFieldSet{
			QueryFields:    []string{"Product"},
			MutationFields: []string{"CreateProduct"},
		},
	}
	layout := resolverLayout{
		Layout: layoutFollowSchema, Dir: "graph", FilenameTemplate: defaultFilenameTpl, Filename: defaultSingleFilename,
	}

	if _, err := writeSeedFiles(opts, layout); err != nil {
		t.Fatalf("writeSeedFiles: %v", err)
	}

	resolverPath := filepath.Join(resolverDir, "resolver.go")
	got, err := os.ReadFile(resolverPath) //nolint:gosec // path is t.TempDir-derived
	if err != nil {
		t.Fatalf("reading scaffold: %v", err)
	}
	gotStr := string(got)

	// goimports orders `graph/sqlgenresolver` before `models` (g < m). Assert
	// the sqlgenresolver import appears BEFORE the models import in the block.
	sqlgenIdx := strings.Index(gotStr, "example.com/foo/graph/sqlgenresolver")
	modelsIdx := strings.Index(gotStr, "example.com/foo/models")
	if sqlgenIdx < 0 || modelsIdx < 0 {
		t.Fatalf("scaffold missing an expected import:\n%s", gotStr)
	}
	if sqlgenIdx > modelsIdx {
		t.Errorf("imports not goimports-sorted: sqlgenresolver must precede models for a top-level resolver dir\n--- output ---\n%s", gotStr)
	}

	// Idempotency (the real-world regen guarantee): a second run finds the
	// file already complete — all fields + the sqlgenresolver import present —
	// so mergeResolverScaffold short-circuits at its `!addedField &&
	// !addedImport` guard and leaves the file untouched. This is exactly what a
	// re-run of `sqlgen graphql gen` does: resolver.go is preserved byte-for-
	// byte across regenerations, so the goimports-sorted first-run output is
	// what the golden captures and what every later regen reproduces.
	if _, err := writeSeedFiles(opts, layout); err != nil {
		t.Fatalf("writeSeedFiles (second run): %v", err)
	}
	got2, err := os.ReadFile(resolverPath) //nolint:gosec // path is t.TempDir-derived
	if err != nil {
		t.Fatalf("reading scaffold after second run: %v", err)
	}
	if string(got2) != gotStr {
		t.Errorf("fresh scaffold and merge path disagree (non-idempotent):\n--- first ---\n%s\n--- second ---\n%s", gotStr, got2)
	}
}

// TestWriteResolverScaffold_MergesIntoExistingFile pins the scaffold's merge
// branch: when resolver.go already exists, writeResolverScaffold AST-
// merges Client / Q / M fields plus the sqlgenresolver import into the
// existing Resolver struct. Mirrors gqlgen's one-shot scaffold shape and
// preserves consumer-added fields verbatim.
func TestWriteResolverScaffold_MergesIntoExistingFile(t *testing.T) {
	dir := t.TempDir()
	resolverDir := filepath.Join(dir, "graph")
	if err := os.MkdirAll(resolverDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Simulate gqlgen's one-shot scaffold: empty Resolver struct. The `models`
	// import is declared up front so the `Client *models.Client` field
	// synthesised by ensureResolverFields lands on an explicit import rather
	// than tripping imports.Process's speculative module-cache scan (symmetric
	// fixture hardening for the mergeResolverScaffold call site).
	// The seed declares the import unused; Go's parser accepts that, and
	// ensureResolverFields adds the Client field that references models BEFORE
	// imports.Process runs, so the import is in-use by the time goimports
	// decides what to keep. gqlgen leaves the Query() / Mutation() methods and
	// the queryResolver / mutationResolver type defs to the schema-source
	// resolver file (typically shared_gen.resolvers.go).
	scaffold := `package graph

import (
	models "example.com/foo/gen"
)

type Resolver struct{}
`
	resolverPath := filepath.Join(resolverDir, "resolver.go")
	if err := os.WriteFile(resolverPath, []byte(scaffold), 0o600); err != nil {
		t.Fatalf("seeding scaffold: %v", err)
	}

	opts := GenOptions{
		WorkingDir:  dir,
		SeedPackage: "graph",
		SeedImports: SeedImports{
			ModelsAlias:              "models",
			ModelsImportPath:         "example.com/foo/gen",
			SqlgenResolverImportPath: "example.com/foo/gen/graph/sqlgenresolver",
		},
		SqlgenManagedFields: SqlgenManagedFieldSet{
			QueryFields:    []string{"Product"},
			MutationFields: []string{"CreateProduct"},
		},
	}

	mergedPath, err := writeResolverScaffold(resolverDir, opts)
	if err != nil {
		t.Fatalf("writeResolverScaffold: %v", err)
	}
	if mergedPath != resolverPath {
		t.Errorf("merged path = %q, want %q", mergedPath, resolverPath)
	}

	got, err := os.ReadFile(resolverPath) //nolint:gosec // path is t.TempDir-derived
	if err != nil {
		t.Fatalf("reading merged resolver.go: %v", err)
	}
	flat := strings.Join(strings.Fields(string(got)), " ")

	wantSubstrings := []string{
		"package graph",
		"Client *models.Client",
		"Q *sqlgenresolver.Q",
		"M *sqlgenresolver.M",
		`"example.com/foo/gen/graph/sqlgenresolver"`,
		// The declared `models` import is preserved after the merge —
		// imports.Process keeps it because the synthesised Client field uses it.
		`"example.com/foo/gen"`,
	}
	for _, sub := range wantSubstrings {
		if !strings.Contains(flat, sub) {
			t.Errorf("merged resolver.go missing %q\n--- output ---\n%s", sub, got)
		}
	}
	// Lock in that imports.Process did NOT speculative-add a second
	// import resolving to a different `models` package from the module cache.
	// The fixture declares exactly one `/foo/gen"` import path; any additional
	// `models`-providing import would fail this guard.
	if n, want := strings.Count(flat, `"example.com/foo/gen"`), 1; n != want {
		t.Errorf("expected %d `example.com/foo/gen` imports, got %d\n--- output ---\n%s", want, n, got)
	}
}

// TestMergeResolverFields_NoMutations pins the scaffold's gating on the merged
// `M` field. When no mutation operations are configured (read-only schema),
// the merge step adds Client / Q only — emitting `M *sqlgenresolver.M`
// would fail to compile because gqlgen's MutationResolver interface is not
// generated for a schema with no mutation fields.
func TestMergeResolverFields_NoMutations(t *testing.T) {
	dir := t.TempDir()
	resolverDir := filepath.Join(dir, "graph")
	if err := os.MkdirAll(resolverDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Same fixture-hardening rationale as
	// TestWriteResolverScaffold_MergesIntoExistingFile: declare `models`
	// up front so the synthesised Client field doesn't trip imports.Process's
	// speculative module-cache scan.
	scaffold := `package graph

import (
	models "example.com/foo/gen"
)

type Resolver struct{}
`
	resolverPath := filepath.Join(resolverDir, "resolver.go")
	if err := os.WriteFile(resolverPath, []byte(scaffold), 0o600); err != nil {
		t.Fatalf("seeding scaffold: %v", err)
	}

	opts := GenOptions{
		WorkingDir:  dir,
		SeedPackage: "graph",
		SeedImports: SeedImports{
			ModelsAlias:              "models",
			ModelsImportPath:         "example.com/foo/gen",
			SqlgenResolverImportPath: "example.com/foo/gen/graph/sqlgenresolver",
		},
		SqlgenManagedFields: SqlgenManagedFieldSet{
			QueryFields:    []string{"Product"},
			MutationFields: nil,
		},
	}

	if _, err := writeResolverScaffold(resolverDir, opts); err != nil {
		t.Fatalf("writeResolverScaffold: %v", err)
	}

	got, err := os.ReadFile(resolverPath) //nolint:gosec // path is t.TempDir-derived
	if err != nil {
		t.Fatalf("reading merged resolver.go: %v", err)
	}
	flat := strings.Join(strings.Fields(string(got)), " ")

	if !strings.Contains(flat, "Q *sqlgenresolver.Q") {
		t.Errorf("merged resolver.go missing Q field\n--- output ---\n%s", got)
	}
	if strings.Contains(flat, "M *sqlgenresolver.M") {
		t.Errorf("merged resolver.go must not add M field when no mutation ops are configured\n--- output ---\n%s", got)
	}
	// Symmetric guard: the declared `models` import survives the
	// merge (synthesised Client field uses it) and imports.Process does NOT
	// speculative-add a second models-providing import from the module cache.
	if !strings.Contains(flat, `"example.com/foo/gen"`) {
		t.Errorf("merged resolver.go missing declared models import\n--- output ---\n%s", got)
	}
	if n, want := strings.Count(flat, `"example.com/foo/gen"`), 1; n != want {
		t.Errorf("expected %d `example.com/foo/gen` imports, got %d\n--- output ---\n%s", want, n, got)
	}
}

// TestMergeResolverFields_PreservesConsumerFields pins the
// scaffold's migration path: when an existing resolver.go already has consumer-
// added fields (e.g. a Logger field plus the Client wiring), the
// post-gqlgen merge adds the missing Q / M fields plus the sqlgenresolver
// import without disturbing any other declarations.
func TestMergeResolverFields_PreservesConsumerFields(t *testing.T) {
	dir := t.TempDir()
	resolverDir := filepath.Join(dir, "graph")
	if err := os.MkdirAll(resolverDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Pre-existing resolver.go with a Client field already
	// present, plus a consumer-added field. No Q / M — those are what we
	// expect the merge to add.
	existing := `package graph

import (
	models "example.com/foo/gen"
)

type Resolver struct {
	Client *models.Client
	Logger any
}

func (r *Resolver) Query() QueryResolver { return &queryResolver{r} }
func (r *Resolver) Mutation() MutationResolver { return &mutationResolver{r} }

type queryResolver struct{ *Resolver }
type mutationResolver struct{ *Resolver }
`
	resolverPath := filepath.Join(resolverDir, "resolver.go")
	if err := os.WriteFile(resolverPath, []byte(existing), 0o600); err != nil {
		t.Fatalf("seeding existing resolver.go: %v", err)
	}

	opts := GenOptions{
		WorkingDir:  dir,
		SeedPackage: "graph",
		SeedImports: SeedImports{
			ModelsAlias:              "models",
			ModelsImportPath:         "example.com/foo/gen",
			SqlgenResolverImportPath: "example.com/foo/gen/graph/sqlgenresolver",
		},
		Seeds: []TableSeed{
			{SnakeName: "products", Body: "// products body\n"},
		},
		SqlgenManagedFields: SqlgenManagedFieldSet{
			QueryFields:    []string{"Product"},
			MutationFields: []string{"CreateProduct"},
		},
	}

	if _, err := writeResolverScaffold(resolverDir, opts); err != nil {
		t.Fatalf("writeResolverScaffold: %v", err)
	}

	got, err := os.ReadFile(resolverPath) //nolint:gosec // path is t.TempDir-derived
	if err != nil {
		t.Fatalf("reading merged resolver.go: %v", err)
	}
	gotStr := string(got)

	// Whitespace-normalise (Go's AST printer uses tabs; the test source has spaces).
	normalize := func(s string) string {
		return strings.Join(strings.Fields(s), " ")
	}
	flat := normalize(gotStr)

	// Q / M fields added.
	if !strings.Contains(flat, "Q *sqlgenresolver.Q") {
		t.Errorf("merged resolver.go missing Q field\n--- output ---\n%s", gotStr)
	}
	if !strings.Contains(flat, "M *sqlgenresolver.M") {
		t.Errorf("merged resolver.go missing M field\n--- output ---\n%s", gotStr)
	}
	// sqlgenresolver import added.
	if !strings.Contains(flat, `"example.com/foo/gen/graph/sqlgenresolver"`) {
		t.Errorf("merged resolver.go missing sqlgenresolver import\n--- output ---\n%s", gotStr)
	}
	// Consumer-added Logger field preserved.
	if !strings.Contains(flat, "Logger any") {
		t.Errorf("merged resolver.go dropped consumer's Logger field\n--- output ---\n%s", gotStr)
	}
	// Original Client field preserved (not duplicated).
	if strings.Count(flat, "Client *models.Client") != 1 {
		t.Errorf("merged resolver.go has %d Client fields (expected 1)\n--- output ---\n%s",
			strings.Count(flat, "Client *models.Client"), gotStr)
	}
	// Existing Query() / Mutation() methods preserved.
	if !strings.Contains(flat, "func (r *Resolver) Query() QueryResolver") {
		t.Errorf("merged resolver.go dropped Query() method\n--- output ---\n%s", gotStr)
	}
}

// TestPostStubRewriter pins the panic-stub rewriter. When an
// existing seed file gains a new sqlgen-managed operation, gqlgen emits a
// panic stub for it; the rewriter converts that stub into a delegation.
// Non-managed names and non-panic bodies are left untouched.
func TestPostStubRewriter(t *testing.T) {
	dir := t.TempDir()
	resolverDir := filepath.Join(dir, "graph")
	if err := os.MkdirAll(resolverDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// imports.Process runs with FormatOnly:false in rewriteOneFile so
	// it can drop the now-unused `fmt` import after every panic stub in the file
	// is rewritten. With FormatOnly:false goimports also scans the module cache
	// to resolve qualified identifiers without a matching import — so an
	// unimported `models.X` reference could be speculatively bound to any
	// module-cache entry that happens to publish a package named `models`. The
	// fixture mirrors a real consumer resolver file by declaring every import
	// the body references (`example.com/models`), eliminating the speculative
	// resolution path entirely.
	original := `package graph

import (
	"context"
	"fmt"

	"example.com/models"
)

// Product is sqlgen-managed and starts as a panic stub — should be rewritten.
func (r *queryResolver) Product(ctx context.Context, id string) (*models.Product, error) {
	panic(fmt.Errorf("not implemented: Product - product"))
}

// CustomNonSqlgen is consumer-authored — must NOT be touched.
func (r *queryResolver) CustomNonSqlgen(ctx context.Context) (int, error) {
	panic("not implemented")
}

// Products is sqlgen-managed and already implemented — must NOT be touched.
func (r *queryResolver) Products(ctx context.Context, filter *ProductFilter, sort []*ProductSort, first *int, after *string, last *int, before *string) (*models.Connection[models.Product], error) {
	return r.Q.Products(ctx, translateProductFilter(filter), first, after, last, before)
}
`
	path := filepath.Join(resolverDir, "products_gen.resolvers.go")
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("seeding file: %v", err)
	}

	opts := GenOptions{
		WorkingDir: dir,
		SqlgenManagedFields: SqlgenManagedFieldSet{
			QueryFields:    []string{"Product", "Products", "ProductList"},
			MutationFields: []string{"CreateProduct", "UpdateProduct", "DeleteProduct"},
		},
	}
	layout := resolverLayout{
		Layout: layoutFollowSchema, Dir: "graph", FilenameTemplate: defaultFilenameTpl, Filename: defaultSingleFilename,
	}

	if err := rewritePanicStubs(opts, layout); err != nil {
		t.Fatalf("rewritePanicStubs: %v", err)
	}

	out, err := os.ReadFile(path) //nolint:gosec // path is t.TempDir-derived
	if err != nil {
		t.Fatalf("reading rewritten file: %v", err)
	}
	got := string(out)

	// Product was a panic stub for a managed name → rewritten to delegation.
	if !strings.Contains(got, "return r.Q.Product(ctx, id)") {
		t.Errorf("Product panic stub was not rewritten to delegation:\n%s", got)
	}
	// CustomNonSqlgen is consumer-authored → panic stub preserved.
	if !strings.Contains(got, `panic("not implemented")`) {
		t.Errorf("consumer-authored CustomNonSqlgen panic stub was unexpectedly rewritten:\n%s", got)
	}
	// Products is already a delegation → preserved.
	if !strings.Contains(got, "return r.Q.Products(ctx, translateProductFilter(filter)") {
		t.Errorf("non-panic Products body was unexpectedly modified:\n%s", got)
	}
	// With the Product rewrite removing the only `fmt` use, the
	// post-rewrite imports.Process drops the unused `fmt` import. `models` is
	// still referenced (Products return type) and must be preserved. The
	// strict count assertion also forbids a *second* speculative-resolved
	// import — if a future Go-version / module-cache state change ever led
	// goimports to bind `models.X` to a path other than the declared
	// `example.com/models`, the resulting file would carry two `/models"`
	// imports and this guard would fire.
	if strings.Contains(got, `"fmt"`) {
		t.Errorf("expected unused fmt import to be dropped after rewrite:\n%s", got)
	}
	if !strings.Contains(got, `"example.com/models"`) {
		t.Errorf("expected example.com/models import to be preserved:\n%s", got)
	}
	if n, want := strings.Count(got, `/models"`), 1; n != want {
		t.Errorf("expected %d `/models\"` imports, got %d (speculative module-cache resolution suspected):\n%s", want, n, got)
	}
}

// TestNoGqlgenDependency is the repo-invariant test that pins PRD §26.5.6's
// "cmd/sqlgen does not import gqlgen" rule. We grep the wrapper package's own
// source for the gqlgen import path; if any file ever imports it, the test
// fails fast.
func TestNoGqlgenDependency(t *testing.T) {
	root := wrapperPackageDir(t)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		data, err := os.ReadFile(path) //nolint:gosec // walking the wrapper package source tree
		if err != nil {
			return err
		}
		if bytes.Contains(data, []byte("github.com/99designs/gqlgen")) && !bytes.Contains(data, []byte("// noimport")) {
			// Allow string-literal references in tests that document the
			// invocation form, but flag actual imports.
			lines := strings.Split(string(data), "\n")
			for i, line := range lines {
				if strings.Contains(line, "github.com/99designs/gqlgen") && strings.Contains(line, "import") {
					return fmt.Errorf("%s:%d imports gqlgen — sqlgen must not depend on gqlgen at compile time", path, i+1)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}
}

func wrapperPackageDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return wd
}

// TestWriteSeedFiles_ReturnsWrittenPaths_FollowSchema pins the reporting
// contract: writeSeedFiles returns the exact file paths it wrote this run so
// the chained `sqlgen generate` report can fold them into the headline file
// count. Pre-existing files (gqlgen-owned after the first run) are excluded —
// re-running must not double-count.
func TestWriteSeedFiles_ReturnsWrittenPaths_FollowSchema(t *testing.T) {
	dir := t.TempDir()
	resolverDir := filepath.Join(dir, "graph")
	if err := os.MkdirAll(resolverDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	opts := GenOptions{
		WorkingDir:  dir,
		SeedPackage: "graph",
		SeedImports: SeedImports{
			ModelsAlias:              "models",
			ModelsImportPath:         "example.com/foo/gen",
			SqlgenResolverImportPath: "example.com/foo/gen/graph/sqlgenresolver",
		},
		Seeds: []TableSeed{
			{SnakeName: "products", Body: "// products body\n"},
			{SnakeName: "users", Body: "// users body\n"},
		},
		SqlgenManagedFields: SqlgenManagedFieldSet{
			QueryFields:    []string{"Product", "User"},
			MutationFields: []string{"CreateProduct"},
		},
	}
	layout := resolverLayout{
		Layout: layoutFollowSchema, Dir: "graph", FilenameTemplate: defaultFilenameTpl, Filename: defaultSingleFilename,
	}

	first, err := writeSeedFiles(opts, layout)
	if err != nil {
		t.Fatalf("writeSeedFiles first run: %v", err)
	}

	// writeSeedFiles emits the slim resolver.go
	// scaffold (struct + Client/Q/M fields, no methods/types) plus the
	// per-table seeds. The headline file count picks up all three.
	wantScaffold := filepath.Join(resolverDir, "resolver.go")
	wantProducts := filepath.Join(resolverDir, "products_gen.resolvers.go")
	wantUsers := filepath.Join(resolverDir, "users_gen.resolvers.go")

	wantSet := map[string]bool{wantScaffold: false, wantProducts: false, wantUsers: false}
	for _, p := range first {
		if _, ok := wantSet[p]; !ok {
			t.Errorf("first run returned unexpected path %q (want subset of %v)", p, wantSet)
			continue
		}
		wantSet[p] = true
	}
	for path, seen := range wantSet {
		if !seen {
			t.Errorf("first run missing path %q (got %v)", path, first)
		}
	}

	for _, p := range []string{wantScaffold, wantProducts, wantUsers} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("first run did not write %q on disk: %v", p, err)
		}
	}

	second, err := writeSeedFiles(opts, layout)
	if err != nil {
		t.Fatalf("writeSeedFiles second run: %v", err)
	}
	if len(second) != 0 {
		t.Errorf("second run returned %v, want empty (all files pre-exist; re-runs must not double-count)", second)
	}
}

// TestWriteSeedFiles_ReturnsWrittenPaths_SingleFile mirrors the
// follow-schema test for the single-file layout. Under single-file the
// wrapper writes one combined seed file at <resolver.filename> on
// the first run and skips the write on subsequent runs.
func TestWriteSeedFiles_ReturnsWrittenPaths_SingleFile(t *testing.T) {
	dir := t.TempDir()
	resolverDir := filepath.Join(dir, "graph")
	if err := os.MkdirAll(resolverDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	opts := GenOptions{
		WorkingDir:  dir,
		SeedPackage: "graph",
		SeedImports: SeedImports{
			ModelsAlias:              "models",
			ModelsImportPath:         "example.com/foo/gen",
			SqlgenResolverImportPath: "example.com/foo/gen/graph/sqlgenresolver",
		},
		Seeds: []TableSeed{
			{SnakeName: "products", Body: "// products body\n"},
		},
	}
	layout := resolverLayout{
		Layout: layoutSingleFile, Dir: "graph", FilenameTemplate: defaultFilenameTpl, Filename: "graph/resolver.go",
	}

	first, err := writeSeedFiles(opts, layout)
	if err != nil {
		t.Fatalf("writeSeedFiles first run: %v", err)
	}
	wantPath := filepath.Join(resolverDir, "resolver.go")
	if len(first) != 1 || first[0] != wantPath {
		t.Errorf("first run = %v, want [%q]", first, wantPath)
	}

	second, err := writeSeedFiles(opts, layout)
	if err != nil {
		t.Fatalf("writeSeedFiles second run: %v", err)
	}
	if len(second) != 0 {
		t.Errorf("second run = %v, want empty", second)
	}
}

// TestMergeResolverScaffold_NoOpWhenFieldsAlreadyPresent pins the
// no-op-merge contract: when the existing resolver.go already declares the
// Q / M fields and imports the sqlgenresolver package, mergeResolverScaffold
// returns (false, nil) without rewriting the file. The caller (mergeResolverFields)
// uses the bool to decide whether to count the path in the seed-file report —
// returning false means the report won't list a file we didn't actually touch.
func TestMergeResolverScaffold_NoOpWhenFieldsAlreadyPresent(t *testing.T) {
	dir := t.TempDir()
	resolverDir := filepath.Join(dir, "graph")
	if err := os.MkdirAll(resolverDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Pre-existing resolver.go with Client + Q + M already wired AND the
	// sqlgenresolver import already present — the shape mergeResolverScaffold
	// would normally produce. Rerunning must NOT touch this file.
	existing := `package graph

import (
	models "example.com/foo/gen"
	sqlgenresolver "example.com/foo/gen/graph/sqlgenresolver"
)

type Resolver struct {
	Client *models.Client
	Q      *sqlgenresolver.Q
	M      *sqlgenresolver.M
}

func (r *Resolver) Query() QueryResolver       { return &queryResolver{r} }
func (r *Resolver) Mutation() MutationResolver { return &mutationResolver{r} }

type queryResolver struct{ *Resolver }
type mutationResolver struct{ *Resolver }
`
	resolverPath := filepath.Join(resolverDir, "resolver.go")
	if err := os.WriteFile(resolverPath, []byte(existing), 0o600); err != nil {
		t.Fatalf("seeding resolver.go: %v", err)
	}
	beforeStat, err := os.Stat(resolverPath)
	if err != nil {
		t.Fatalf("stat before: %v", err)
	}
	beforeBytes, err := os.ReadFile(resolverPath) //nolint:gosec // t.TempDir-derived
	if err != nil {
		t.Fatalf("read before: %v", err)
	}

	imps := SeedImports{
		ModelsAlias:              "models",
		ModelsImportPath:         "example.com/foo/gen",
		SqlgenResolverImportPath: "example.com/foo/gen/graph/sqlgenresolver",
	}

	rewritten, err := mergeResolverScaffold(resolverPath, imps, true)
	if err != nil {
		t.Fatalf("mergeResolverScaffold: %v", err)
	}
	if rewritten {
		t.Errorf("rewritten = true, want false (file already had Q/M + sqlgenresolver import — no edits needed)")
	}

	afterStat, err := os.Stat(resolverPath)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	if !afterStat.ModTime().Equal(beforeStat.ModTime()) {
		t.Errorf("file mtime changed (%v -> %v), want unchanged on no-op merge", beforeStat.ModTime(), afterStat.ModTime())
	}
	afterBytes, err := os.ReadFile(resolverPath) //nolint:gosec // t.TempDir-derived
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if !bytes.Equal(beforeBytes, afterBytes) {
		t.Errorf("file contents changed on no-op merge\nbefore:\n%s\nafter:\n%s", beforeBytes, afterBytes)
	}
}

// TestMergeResolverFields_NoOpReturnsEmptyPath asserts mergeResolverFields
// returns ("", nil) when the existing resolver.go already declares the
// Q / M fields and imports sqlgenresolver — no edits needed, so the path
// must NOT appear in the GenResult.SeedFiles list.
func TestMergeResolverFields_NoOpReturnsEmptyPath(t *testing.T) {
	dir := t.TempDir()
	resolverDir := filepath.Join(dir, "graph")
	if err := os.MkdirAll(resolverDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	existing := `package graph

import (
	models "example.com/foo/gen"
	sqlgenresolver "example.com/foo/gen/graph/sqlgenresolver"
)

type Resolver struct {
	Client *models.Client
	Q      *sqlgenresolver.Q
	M      *sqlgenresolver.M
}

func (r *Resolver) Query() QueryResolver       { return &queryResolver{r} }
func (r *Resolver) Mutation() MutationResolver { return &mutationResolver{r} }

type queryResolver struct{ *Resolver }
type mutationResolver struct{ *Resolver }
`
	resolverPath := filepath.Join(resolverDir, "resolver.go")
	if err := os.WriteFile(resolverPath, []byte(existing), 0o600); err != nil {
		t.Fatalf("seeding resolver.go: %v", err)
	}

	opts := GenOptions{
		WorkingDir:  dir,
		SeedPackage: "graph",
		SeedImports: SeedImports{
			ModelsAlias:              "models",
			ModelsImportPath:         "example.com/foo/gen",
			SqlgenResolverImportPath: "example.com/foo/gen/graph/sqlgenresolver",
		},
		SqlgenManagedFields: SqlgenManagedFieldSet{
			QueryFields:    []string{"Product"},
			MutationFields: []string{"CreateProduct"},
		},
	}
	got, err := writeResolverScaffold(resolverDir, opts)
	if err != nil {
		t.Fatalf("writeResolverScaffold: %v", err)
	}
	if got != "" {
		t.Errorf("path = %q, want \"\" (no-op merge means no path in the seed-file report)", got)
	}
}

// TestWriteResolverScaffold_AbsentFile_WritesSlimScaffold pins the
// scaffold's greenfield path: when resolver.go is absent, the wrapper writes the
// slim scaffold (struct + Client/Q/M fields) rather than failing or
// no-op'ing. This is the typical path on a consumer's first
// `sqlgen graphql gen` run.
func TestWriteResolverScaffold_AbsentFile_WritesSlimScaffold(t *testing.T) {
	dir := t.TempDir()
	resolverDir := filepath.Join(dir, "graph")
	if err := os.MkdirAll(resolverDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	opts := GenOptions{
		WorkingDir:  dir,
		SeedPackage: "graph",
		SeedImports: SeedImports{
			ModelsAlias:              "models",
			ModelsImportPath:         "example.com/foo/gen",
			SqlgenResolverImportPath: "example.com/foo/gen/graph/sqlgenresolver",
		},
		SqlgenManagedFields: SqlgenManagedFieldSet{
			QueryFields:    []string{"Product"},
			MutationFields: []string{"CreateProduct"},
		},
	}
	got, err := writeResolverScaffold(resolverDir, opts)
	if err != nil {
		t.Fatalf("writeResolverScaffold: %v", err)
	}
	resolverPath := filepath.Join(resolverDir, "resolver.go")
	if got != resolverPath {
		t.Errorf("path = %q, want %q", got, resolverPath)
	}

	body, err := os.ReadFile(resolverPath) //nolint:gosec // path is t.TempDir-derived
	if err != nil {
		t.Fatalf("reading scaffold: %v", err)
	}
	flat := strings.Join(strings.Fields(string(body)), " ")
	for _, sub := range []string{"type Resolver struct {", "Client *models.Client", "Q *sqlgenresolver.Q", "M *sqlgenresolver.M"} {
		if !strings.Contains(flat, sub) {
			t.Errorf("scaffold missing %q\n--- output ---\n%s", sub, body)
		}
	}
}

// TestMergeSingleFileResolver_RestoresWipedFields pins the post-gqlgen
// merge under single-file layout. Simulates gqlgen v0.17.90's resolvergen
// pass: the seeded `Resolver struct { Client; Q; M }` is wiped back to
// `Resolver struct{}` while the per-table method bodies (which reference
// `r.Q.X(...)` / `r.M.X(...)`) are preserved. The merge must restore Client
// / Q / M and re-add the sqlgenresolver import so the package compiles.
func TestMergeSingleFileResolver_RestoresWipedFields(t *testing.T) {
	dir := t.TempDir()
	resolverDir := filepath.Join(dir, "graph")
	if err := os.MkdirAll(resolverDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Shape gqlgen produces under single-file when it owns the file: empty
	// Resolver struct, queryResolver / mutationResolver type defs, plus
	// preserved per-table method bodies. Models import survives because
	// method signatures reference `*models.Product`; sqlgenresolver is
	// dropped because the wiped struct no longer references it.
	wiped := `package graph

import (
	"context"

	models "example.com/foo/gen"
)

type Resolver struct{}

func (r *Resolver) Query() QueryResolver       { return &queryResolver{r} }
func (r *Resolver) Mutation() MutationResolver { return &mutationResolver{r} }

type queryResolver struct{ *Resolver }
type mutationResolver struct{ *Resolver }

func (r *queryResolver) Product(ctx context.Context, id string) (*models.Product, error) {
	return r.Q.Product(ctx, id)
}

func (r *mutationResolver) CreateProduct(ctx context.Context, input models.CreateProductInput) (*models.Product, error) {
	return r.M.CreateProduct(ctx, input)
}
`
	resolverPath := filepath.Join(resolverDir, "resolver.go")
	if err := os.WriteFile(resolverPath, []byte(wiped), 0o600); err != nil {
		t.Fatalf("seeding wiped resolver.go: %v", err)
	}

	opts := GenOptions{
		WorkingDir: dir,
		SeedImports: SeedImports{
			ModelsAlias:              "models",
			ModelsImportPath:         "example.com/foo/gen",
			SqlgenResolverImportPath: "example.com/foo/gen/graph/sqlgenresolver",
		},
		SqlgenManagedFields: SqlgenManagedFieldSet{
			QueryFields:    []string{"Product"},
			MutationFields: []string{"CreateProduct"},
		},
	}
	layout := resolverLayout{
		Layout: layoutSingleFile, Dir: "graph", FilenameTemplate: defaultFilenameTpl, Filename: "graph/resolver.go",
	}

	got, err := mergeSingleFileResolver(opts, layout)
	if err != nil {
		t.Fatalf("mergeSingleFileResolver: %v", err)
	}
	if got != resolverPath {
		t.Errorf("returned path = %q, want %q", got, resolverPath)
	}

	merged, err := os.ReadFile(resolverPath) //nolint:gosec // path is t.TempDir-derived
	if err != nil {
		t.Fatalf("reading merged resolver.go: %v", err)
	}
	flat := strings.Join(strings.Fields(string(merged)), " ")

	for _, sub := range []string{
		"Client *models.Client",
		"Q *sqlgenresolver.Q",
		"M *sqlgenresolver.M",
		`"example.com/foo/gen/graph/sqlgenresolver"`,
	} {
		if !strings.Contains(flat, sub) {
			t.Errorf("merged resolver.go missing %q\n--- output ---\n%s", sub, merged)
		}
	}

	// Preserved gqlgen-emitted shapes must NOT be disturbed.
	for _, sub := range []string{
		"func (r *Resolver) Query() QueryResolver",
		"func (r *Resolver) Mutation() MutationResolver",
		"type queryResolver struct{ *Resolver }",
		"type mutationResolver struct{ *Resolver }",
		"return r.Q.Product(ctx, id)",
		"return r.M.CreateProduct(ctx, input)",
	} {
		if !strings.Contains(string(merged), sub) {
			t.Errorf("merge dropped gqlgen-emitted shape %q\n--- output ---\n%s", sub, merged)
		}
	}
}

// TestMergeSingleFileResolver_FollowSchemaIsNoOp pins the layout gate.
// The post-gqlgen merge is single-file-only; under follow-schema the
// equivalent merge already runs BEFORE the subprocess (writeResolverScaffold)
// because gqlgen does not own resolver.go there. Calling
// mergeSingleFileResolver under follow-schema must return ("", nil) without
// touching disk.
func TestMergeSingleFileResolver_FollowSchemaIsNoOp(t *testing.T) {
	dir := t.TempDir()
	resolverDir := filepath.Join(dir, "graph")
	if err := os.MkdirAll(resolverDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Even with a wipe-shaped resolver.go on disk, follow-schema layout
	// must skip the merge — that path is owned by writeResolverScaffold.
	wiped := `package graph

type Resolver struct{}
`
	resolverPath := filepath.Join(resolverDir, "resolver.go")
	if err := os.WriteFile(resolverPath, []byte(wiped), 0o600); err != nil {
		t.Fatalf("seeding resolver.go: %v", err)
	}
	beforeBytes, err := os.ReadFile(resolverPath) //nolint:gosec // t.TempDir-derived
	if err != nil {
		t.Fatalf("read before: %v", err)
	}

	opts := GenOptions{
		WorkingDir: dir,
		SeedImports: SeedImports{
			ModelsAlias:              "models",
			ModelsImportPath:         "example.com/foo/gen",
			SqlgenResolverImportPath: "example.com/foo/gen/graph/sqlgenresolver",
		},
		SqlgenManagedFields: SqlgenManagedFieldSet{MutationFields: []string{"CreateProduct"}},
	}
	layout := resolverLayout{
		Layout: layoutFollowSchema, Dir: "graph", FilenameTemplate: defaultFilenameTpl, Filename: defaultSingleFilename,
	}

	got, err := mergeSingleFileResolver(opts, layout)
	if err != nil {
		t.Fatalf("mergeSingleFileResolver: %v", err)
	}
	if got != "" {
		t.Errorf("returned path = %q, want \"\" (follow-schema must be a no-op)", got)
	}
	afterBytes, err := os.ReadFile(resolverPath) //nolint:gosec // t.TempDir-derived
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if !bytes.Equal(beforeBytes, afterBytes) {
		t.Errorf("file contents changed under follow-schema no-op\nbefore:\n%s\nafter:\n%s", beforeBytes, afterBytes)
	}
}

// TestMergeSingleFileResolver_AbsentFile pins the missing-file branch:
// when the destination doesn't exist (e.g. the gqlgen subprocess errored
// out before producing it, though in practice writeSingleFileSeed already
// created it before the subprocess), the merge returns ("", nil) without
// erroring.
func TestMergeSingleFileResolver_AbsentFile(t *testing.T) {
	dir := t.TempDir()
	opts := GenOptions{
		WorkingDir: dir,
		SeedImports: SeedImports{
			ModelsAlias:              "models",
			ModelsImportPath:         "example.com/foo/gen",
			SqlgenResolverImportPath: "example.com/foo/gen/graph/sqlgenresolver",
		},
	}
	layout := resolverLayout{
		Layout: layoutSingleFile, Dir: "graph", FilenameTemplate: defaultFilenameTpl, Filename: "graph/resolver.go",
	}

	got, err := mergeSingleFileResolver(opts, layout)
	if err != nil {
		t.Fatalf("mergeSingleFileResolver: %v", err)
	}
	if got != "" {
		t.Errorf("returned path = %q, want \"\" (no file to merge)", got)
	}
}

// TestMergeSingleFileResolver_NoOpWhenFieldsAlreadyPresent pins the
// idempotent-merge contract: when the file already has Client / Q / M
// (e.g. a prior wrapper run already restored them and the next gqlgen run
// happened to preserve them), the merge returns ("", nil) without
// rewriting the file.
func TestMergeSingleFileResolver_NoOpWhenFieldsAlreadyPresent(t *testing.T) {
	dir := t.TempDir()
	resolverDir := filepath.Join(dir, "graph")
	if err := os.MkdirAll(resolverDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	existing := `package graph

import (
	models "example.com/foo/gen"
	sqlgenresolver "example.com/foo/gen/graph/sqlgenresolver"
)

type Resolver struct {
	Client *models.Client
	Q      *sqlgenresolver.Q
	M      *sqlgenresolver.M
}
`
	resolverPath := filepath.Join(resolverDir, "resolver.go")
	if err := os.WriteFile(resolverPath, []byte(existing), 0o600); err != nil {
		t.Fatalf("seeding resolver.go: %v", err)
	}
	beforeStat, err := os.Stat(resolverPath)
	if err != nil {
		t.Fatalf("stat before: %v", err)
	}

	opts := GenOptions{
		WorkingDir: dir,
		SeedImports: SeedImports{
			ModelsAlias:              "models",
			ModelsImportPath:         "example.com/foo/gen",
			SqlgenResolverImportPath: "example.com/foo/gen/graph/sqlgenresolver",
		},
		SqlgenManagedFields: SqlgenManagedFieldSet{MutationFields: []string{"CreateProduct"}},
	}
	layout := resolverLayout{
		Layout: layoutSingleFile, Dir: "graph", FilenameTemplate: defaultFilenameTpl, Filename: "graph/resolver.go",
	}

	got, err := mergeSingleFileResolver(opts, layout)
	if err != nil {
		t.Fatalf("mergeSingleFileResolver: %v", err)
	}
	if got != "" {
		t.Errorf("returned path = %q, want \"\" (no fields to add)", got)
	}
	afterStat, err := os.Stat(resolverPath)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	if !afterStat.ModTime().Equal(beforeStat.ModTime()) {
		t.Errorf("file mtime changed (%v -> %v), want unchanged on no-op merge", beforeStat.ModTime(), afterStat.ModTime())
	}
}

// TestPostStubRewriter_PrefersSeedBody pins the rewriter's use of the
// rendered seed body when one exists for the stubbed method.
//
// Seed files are write-once: once `<table>_gen.resolvers.go` exists gqlgen
// owns it, so a table that later gains a sqlgen-managed operation gets a
// gqlgen panic stub rather than the seeded body. The synthesised delegation
// passes parameters straight through, which is only correct for operations
// whose arguments need no translation — a create input must be routed
// through translateCreate<T>Input first or the model receives the wrong
// type entirely. The seed body is the only source that knows this.
func TestPostStubRewriter_PrefersSeedBody(t *testing.T) {
	dir := t.TempDir()
	resolverDir := filepath.Join(dir, "graph")
	if err := os.MkdirAll(resolverDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// An existing resolver file that predates the create surface: gqlgen has
	// just added a panic stub for the newly-schema'd createUserCategory.
	original := `package graph

import (
	"context"
	"fmt"

	"example.com/models"
)

// CreateUserCategory is the resolver for the createUserCategory field.
func (r *mutationResolver) CreateUserCategory(ctx context.Context, input CreateUserCategoryInput) (*models.UserCategory, error) {
	panic(fmt.Errorf("not implemented: CreateUserCategory - createUserCategory"))
}

// DeleteUserCategory was seeded on the first run and must not be disturbed.
func (r *mutationResolver) DeleteUserCategory(ctx context.Context, userID string, categoryID int) (bool, error) {
	return r.M.DeleteUserCategory(ctx, userID, categoryID)
}
`
	path := filepath.Join(resolverDir, "user_category_gen.resolvers.go")
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("seeding file: %v", err)
	}

	opts := GenOptions{
		WorkingDir: dir,
		SqlgenManagedFields: SqlgenManagedFieldSet{
			MutationFields: []string{"CreateUserCategory", "DeleteUserCategory", "UnseededMutation"},
		},
		Seeds: []TableSeed{{
			SnakeName: "user_category",
			Body: `
// CreateUserCategory is the resolver for the createUserCategory field.
func (r *mutationResolver) CreateUserCategory(ctx context.Context, input CreateUserCategoryInput) (*models.UserCategory, error) {
	return r.M.CreateUserCategory(ctx, translateCreateUserCategoryInput(input))
}
`,
		}},
	}
	layout := resolverLayout{
		Layout: layoutFollowSchema, Dir: "graph", FilenameTemplate: defaultFilenameTpl, Filename: defaultSingleFilename,
	}

	if err := rewritePanicStubs(opts, layout); err != nil {
		t.Fatalf("rewritePanicStubs: %v", err)
	}

	out, err := os.ReadFile(path) //nolint:gosec // path is t.TempDir-derived
	if err != nil {
		t.Fatalf("reading rewritten file: %v", err)
	}
	got := string(out)

	// The seeded body wins over the synthesised pass-through delegation.
	if !strings.Contains(got, "return r.M.CreateUserCategory(ctx, translateCreateUserCategoryInput(input))") {
		t.Errorf("stub was not rewritten to the seeded body:\n%s", got)
	}
	if strings.Contains(got, "return r.M.CreateUserCategory(ctx, input)") {
		t.Errorf("stub was rewritten to the synthesised pass-through delegation:\n%s", got)
	}
	// Splicing must not disturb the file's own comments. Each doc comment
	// stays immediately above the function it documents.
	for _, want := range []string{
		"// CreateUserCategory is the resolver for the createUserCategory field.\nfunc (r *mutationResolver) CreateUserCategory(",
		"// DeleteUserCategory was seeded on the first run and must not be disturbed.\nfunc (r *mutationResolver) DeleteUserCategory(",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("comment placement disturbed, missing:\n%s\ngot:\n%s", want, got)
		}
	}
	// The already-implemented method is untouched.
	if !strings.Contains(got, "return r.M.DeleteUserCategory(ctx, userID, categoryID)") {
		t.Errorf("existing delegation was modified:\n%s", got)
	}
}

// TestPostStubRewriter_FallsBackWhenUnseeded keeps the synthesised
// delegation for a managed stub with no rendered seed body — the behavior
// that existed before seed bodies were consulted.
func TestPostStubRewriter_FallsBackWhenUnseeded(t *testing.T) {
	dir := t.TempDir()
	resolverDir := filepath.Join(dir, "graph")
	if err := os.MkdirAll(resolverDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	original := `package graph

import (
	"context"
	"fmt"

	"example.com/models"
)

func (r *queryResolver) Product(ctx context.Context, id string) (*models.Product, error) {
	panic(fmt.Errorf("not implemented: Product - product"))
}
`
	path := filepath.Join(resolverDir, "product_gen.resolvers.go")
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("seeding file: %v", err)
	}

	opts := GenOptions{
		WorkingDir:          dir,
		SqlgenManagedFields: SqlgenManagedFieldSet{QueryFields: []string{"Product"}},
		// A seed for a different table — no entry for queryResolver.Product.
		Seeds: []TableSeed{{SnakeName: "order", Body: "\nfunc (r *queryResolver) Order(ctx context.Context, id string) error { return nil }\n"}},
	}
	layout := resolverLayout{
		Layout: layoutFollowSchema, Dir: "graph", FilenameTemplate: defaultFilenameTpl, Filename: defaultSingleFilename,
	}

	if err := rewritePanicStubs(opts, layout); err != nil {
		t.Fatalf("rewritePanicStubs: %v", err)
	}

	out, err := os.ReadFile(path) //nolint:gosec // path is t.TempDir-derived
	if err != nil {
		t.Fatalf("reading rewritten file: %v", err)
	}
	if got := string(out); !strings.Contains(got, "return r.Q.Product(ctx, id)") {
		t.Errorf("unseeded stub did not fall back to the synthesised delegation:\n%s", got)
	}
}
