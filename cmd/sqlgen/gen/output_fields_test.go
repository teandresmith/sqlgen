package gen_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/manifest"
	"github.com/teandresmith/sqlgen/parser"
)

// tdfDDL declares an enum and a composite type, so a run writes both the
// enums file and the types file.
const tdfDDL = `
CREATE TYPE status AS ENUM ('active', 'archived');
CREATE TYPE address AS (street text, city text);
CREATE TABLE accounts (id uuid PRIMARY KEY, status status NOT NULL, home address);`

// tdfProject writes tdfDDL and a sqlgen.yml whose output block is output
// (given the project root) into a fresh temp dir, moves into it, and returns
// the root, the parsed schema and the loaded config. A config that fails
// pre-parse validation fails the test, so every run here is one the CLI
// would make.
func tdfProject(t *testing.T, output func(root string) string, extra string) (string, *parser.Schema, *config.RootConfig) {
	t.Helper()
	root := t.TempDir()
	cfgYAML := "version: v1\ninput:\n  dialect: postgres\n  paths: [./schema.sql]\n" + extra + "output:\n  dir: ./models\n" + output(root)
	for name, body := range map[string]string{"schema.sql": tdfDDL, "sqlgen.yml": cfgYAML} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	t.Chdir(root)

	cfg, err := config.LoadConfig(filepath.Join(root, "sqlgen.yml"))
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}
	if _, err := config.ValidatePreParse(cfg); err != nil {
		t.Fatalf("ValidatePreParse() error: %v", err)
	}
	return root, parseDDLForTest(t, config.DialectPostgres, tdfDDL), cfg
}

// tdfGenerate runs Generate and fails the test on an error.
func tdfGenerate(t *testing.T, schema *parser.Schema, cfg *config.RootConfig) *gen.GenerateResult {
	t.Helper()
	result, err := gen.Generate(schema, cfg, "test")
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}
	return result
}

// tdfFileExists reports whether path names an existing file.
func tdfFileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// tdfPackageClause returns the package clause line of the Go file at path.
func tdfPackageClause(t *testing.T, path string) string {
	t.Helper()
	src, err := os.ReadFile(path) //nolint:gosec // path is under the test's temp dir
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	for line := range strings.SplitSeq(string(src), "\n") {
		if strings.HasPrefix(line, "package ") {
			return line
		}
	}
	t.Fatalf("%s has no package clause", path)
	return ""
}

// TestGenerate_ExplicitEnumAndTypeFileNamesNameTheFiles pins §4.5: an
// explicit relative output.enums.file or output.types.file is the name the
// file is written under in output.dir, and the default name is not written.
func TestGenerate_ExplicitEnumAndTypeFileNamesNameTheFiles(t *testing.T) {
	root, schema, cfg := tdfProject(t, func(string) string {
		return "  enums:\n    file: pg_enums_gen.go\n  types:\n    file: pg_types_gen.go\n"
	}, "")
	tdfGenerate(t, schema, cfg)

	tests := []struct {
		name        string
		want        string
		defaultName string
	}{
		{name: "enums file", want: "pg_enums_gen.go", defaultName: "enums_gen.go"},
		{name: "types file", want: "pg_types_gen.go", defaultName: "types_gen.go"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tdfFileExists(filepath.Join(root, "models", tt.want)) {
				t.Errorf("Generate() did not write models/%s", tt.want)
			}
			if tdfFileExists(filepath.Join(root, "models", tt.defaultName)) {
				t.Errorf("Generate() wrote models/%s, want only the configured name", tt.defaultName)
			}
		})
	}
}

// TestGenerate_AbsoluteEnumAndTypeFilesInOutputDirAreUsedAsWritten pins
// §4.5: an absolute output.enums.file or output.types.file is the path the
// file is written to, not one joined under output.dir. The paths sit inside
// output.dir, the only place the file may be (§4.5 models-package rule).
func TestGenerate_AbsoluteEnumAndTypeFilesInOutputDirAreUsedAsWritten(t *testing.T) {
	root, schema, cfg := tdfProject(t, func(root string) string {
		return "  enums:\n    file: " + filepath.Join(root, "models", "abs_enums_gen.go") +
			"\n  types:\n    file: " + filepath.Join(root, "models", "abs_types_gen.go") + "\n"
	}, "")
	tdfGenerate(t, schema, cfg)

	for _, tt := range []struct {
		name string
		file string
	}{
		{name: "enums file", file: "abs_enums_gen.go"},
		{name: "types file", file: "abs_types_gen.go"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			want := filepath.Join(root, "models", tt.file)
			if !tdfFileExists(want) {
				t.Errorf("Generate() did not write %s at its absolute path", want)
			}
			if joined := filepath.Join(root, "models", want); tdfFileExists(joined) {
				t.Errorf("Generate() wrote %s, the absolute path joined under output.dir", joined)
			}
		})
	}
}

// TestGenerate_ExplicitEnumAndTypePackageNameTheFiles pins §4.5: an explicit
// output.enums.package or output.types.package, which must equal
// output.package, is accepted and is the package clause of that file.
// output.dir's base is "models", so a name derived from the directory would
// not match.
func TestGenerate_ExplicitEnumAndTypePackageNameTheFiles(t *testing.T) {
	root, schema, cfg := tdfProject(t, func(string) string {
		return "  package: store\n  enums:\n    package: store\n  types:\n    package: store\n"
	}, "")
	tdfGenerate(t, schema, cfg)

	for _, tt := range []struct {
		name string
		file string
	}{
		{name: "enums file", file: "enums_gen.go"},
		{name: "types file", file: "types_gen.go"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tdfPackageClause(t, filepath.Join(root, "models", tt.file)); got != "package store" {
				t.Errorf("models/%s package clause = %q, want %q", tt.file, got, "package store")
			}
		})
	}
}

// TestGenerate_ExplicitClientNameNamesTheStruct pins §4.5: an explicit
// output.client.name is the name of the unified client struct.
func TestGenerate_ExplicitClientNameNamesTheStruct(t *testing.T) {
	root, schema, cfg := tdfProject(t, func(string) string {
		return "  client:\n    name: Store\n"
	}, "")
	tdfGenerate(t, schema, cfg)

	src, err := os.ReadFile(filepath.Join(root, "models", "client_gen.go")) //nolint:gosec // path is under the test's temp dir
	if err != nil {
		t.Fatalf("reading models/client_gen.go: %v", err)
	}
	if !strings.Contains(string(src), "\ntype Store struct {\n") {
		t.Errorf("models/client_gen.go does not declare %q", "type Store struct {")
	}
	if strings.Contains(string(src), "\ntype Client struct {\n") {
		t.Errorf("models/client_gen.go declares the default %q", "type Client struct {")
	}
}

// TestGenerate_ConnectionNeverReachesGeneratedCodeOrManifest pins §4.1.1:
// no input.connection value, written literally or through a ${VAR}
// reference, appears in any generated file or manifest artifact.
func TestGenerate_ConnectionNeverReachesGeneratedCodeOrManifest(t *testing.T) {
	t.Setenv("SQLGEN_DB_URL", "")
	t.Setenv("SQLGEN_DB_PASSWORD", "")
	t.Setenv("TDF_CONN_PASSWORD", "tdfconnpassword")
	const connection = "  connection:\n" +
		"    url: postgres://tdfconnurluser@tdfconnurlhost/tdfconnurldb\n" +
		"    host: tdfconnhost\n" +
		"    port: 61987\n" +
		"    user: tdfconnuser\n" +
		"    password: ${TDF_CONN_PASSWORD}\n" +
		"    database: tdfconndb\n" +
		"    ssl_mode: tdfconnsslmode\n" +
		"generation:\n  manifest:\n    enabled: true\n    embed_in_client: true\n"
	root, schema, cfg := tdfProject(t, func(string) string { return "" }, connection)
	if cfg.Input.Connection == nil || cfg.Input.Connection.Password != "tdfconnpassword" {
		t.Fatalf("LoadConfig() input.connection = %+v, want the expanded password", cfg.Input.Connection)
	}

	result := tdfGenerate(t, schema, cfg)
	if _, err := manifest.RunStage(manifest.StageInput{
		Schema:    schema,
		Config:    cfg,
		Tables:    result.Tables,
		Views:     result.Views,
		Enums:     result.Enums,
		API:       result.API,
		UUID:      result.UUID,
		Version:   "test",
		Timestamp: "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("manifest.RunStage() error: %v", err)
	}

	var written []string
	err := filepath.WalkDir(filepath.Join(root, "models"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		written = append(written, path)
		src, err := os.ReadFile(path) //nolint:gosec // path is under the test's temp dir
		if err != nil {
			return err
		}
		if strings.Contains(string(src), "tdfconn") || strings.Contains(string(src), "61987") {
			rel, _ := filepath.Rel(root, path)
			t.Errorf("%s holds an input.connection value", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking models: %v", err)
	}
	if !tdfFileExists(filepath.Join(root, "models", "manifest", "manifest_gen.json")) {
		t.Fatalf("manifest_gen.json not written; written = %q", written)
	}
}
