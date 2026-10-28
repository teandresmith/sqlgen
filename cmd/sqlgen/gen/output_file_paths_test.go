package gen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

const opEnumTypeDDL = `
CREATE TYPE status AS ENUM ('active', 'archived');
CREATE TYPE address AS (street text, city text);
CREATE TABLE accounts (id uuid PRIMARY KEY, status status NOT NULL, home address);`

// opAbsoluteFileProject writes a postgres project into a fresh temp dir whose
// output.enums.file and output.types.file are the given paths, moves into it,
// and returns the root, the parsed schema and the loaded config. extra is
// appended to the output block. The config must pass pre-parse validation, so
// each file has to resolve into output.dir.
func opAbsoluteFileProject(t *testing.T, ddl string, enumsFile, typesFile func(root string) string, extra string) (string, *parser.Schema, *config.RootConfig) {
	t.Helper()
	root := t.TempDir()
	cfgYAML := "version: v1\ninput:\n  dialect: postgres\n  paths: [./schema.sql]\noutput:\n  dir: ./models\n  package: models\n" + extra +
		"  enums:\n    file: " + enumsFile(root) + "\n  types:\n    file: " + typesFile(root) + "\n"
	for name, body := range map[string]string{"schema.sql": ddl, "sqlgen.yml": cfgYAML} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	t.Chdir(root)

	schema := parseDDLForTest(t, config.DialectPostgres, ddl)
	cfg, err := config.LoadConfig(filepath.Join(root, "sqlgen.yml"))
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}
	if _, err := config.ValidatePreParse(cfg); err != nil {
		t.Fatalf("ValidatePreParse() error = %v, want the fixture config to be valid", err)
	}
	return root, schema, cfg
}

// opInModels names a file in output.dir by its absolute path. A writer that
// joined an absolute path onto output.dir would put it at
// models/<root>/models/<name> instead.
func opInModels(name string) func(string) string {
	return func(root string) string { return filepath.Join(root, "models", name) }
}

// TestGenerateInto_AbsoluteEnumAndTypeFilesStayInTheSandbox pins the preview
// half: a redirected run writes an absolute enums or types file under the
// write root, never at the real path, and WriteDirs maps the preview file
// back to the absolute directory it would occupy for real.
func TestGenerateInto_AbsoluteEnumAndTypeFilesStayInTheSandbox(t *testing.T) {
	// Non-default names, so a run that dropped the absolute path for the
	// default file name would pair nothing.
	root, schema, cfg := opAbsoluteFileProject(t, opEnumTypeDDL, opInModels("abs_enums_gen.go"), opInModels("abs_types_gen.go"), "")
	writeRoot := t.TempDir()

	result, err := gen.GenerateInto(schema, cfg, "test", writeRoot)
	if err != nil {
		t.Fatalf("GenerateInto() error: %v", err)
	}

	realDir := filepath.Join(root, "models")
	if _, err := os.Stat(realDir); err == nil {
		t.Errorf("a redirected run created %s outside the write root", realDir)
	}
	paired := map[string]bool{}
	for _, f := range result.Files {
		if !strings.HasPrefix(f, writeRoot+string(filepath.Separator)) {
			t.Errorf("a redirected run wrote %s outside the write root %s", f, writeRoot)
			continue
		}
		dir, ok := result.WriteDirs[filepath.Dir(f)]
		if !ok {
			t.Errorf("WriteDirs has no entry for %s, so a preview cannot pair it", filepath.Dir(f))
			continue
		}
		paired[filepath.Join(dir, filepath.Base(f))] = true
	}
	for _, want := range []string{opInModels("abs_enums_gen.go")(root), opInModels("abs_types_gen.go")(root)} {
		if !paired[want] {
			t.Errorf("no preview file pairs with %s; paired: %v", want, paired)
		}
	}
}

// TestStaleFiles_SameNameWrittenElsewhereDoesNotProtect pins that only a file
// written into the swept directory keeps that directory's file of the same
// name: a same-named file written to another directory (the graph package,
// say) leaves output.dir's copy from an earlier run orphaned, while the file
// written into output.dir protects it however the path is spelled.
func TestStaleFiles_SameNameWrittenElsewhereDoesNotProtect(t *testing.T) {
	root := t.TempDir()
	outputDir := filepath.Join(root, "models")
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		t.Fatal(err)
	}
	leftover := filepath.Join(outputDir, "enums_gen.go")
	if err := os.WriteFile(leftover, []byte(gen.Header("dev")+"\n\npackage models\n"), 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	stale, err := gen.StaleFiles(outputDir, []string{filepath.Join(root, "graph", "enums_gen.go")})
	if err != nil {
		t.Fatalf("StaleFiles() unexpected error: %v", err)
	}
	if len(stale) != 1 || stale[0] != leftover {
		t.Errorf("StaleFiles() = %v, want [%s]", stale, leftover)
	}

	// The same file named relative to the working directory, or by its
	// absolute path against a relative output.dir, still counts.
	t.Chdir(root)
	for _, written := range []string{filepath.Join("models", "enums_gen.go"), leftover} {
		stale, err = gen.StaleFiles("models", []string{written})
		if err != nil {
			t.Fatalf("StaleFiles() unexpected error: %v", err)
		}
		if len(stale) != 0 {
			t.Errorf("StaleFiles(%q) = %v, want empty — the file was written into the swept directory", written, stale)
		}
	}
}

// TestGenerate_AbsoluteEnumFileInOutputDirReservesItsStem pins that an
// absolute output.enums.file landing in output.dir reserves its stem like a
// relative one, so a table whose file_per_table file would overwrite it is
// refused. The enums file is named account_gen.go, the file the accounts
// table resolves to.
func TestGenerate_AbsoluteEnumFileInOutputDirReservesItsStem(t *testing.T) {
	const ddl = `
CREATE TYPE status AS ENUM ('active', 'archived');
CREATE TABLE accounts (id uuid PRIMARY KEY, status status NOT NULL);`
	_, schema, cfg := opAbsoluteFileProject(t, ddl, opInModels("account_gen.go"), opInModels("types_gen.go"), "  layout: file_per_table\n")
	_, err := gen.Generate(schema, cfg, "test")
	if err == nil || !strings.Contains(err.Error(), "account_gen.go") {
		t.Errorf("Generate() error = %v, want a file-stem collision naming account_gen.go", err)
	}
}
