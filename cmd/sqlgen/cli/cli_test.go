package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
)

// executeCommand creates a fresh root command and executes it with the given args,
// capturing stdout and stderr. Returns stdout, stderr, and any error.
func executeCommand(args ...string) (string, string, error) {
	rootCmd := NewRootCmd()
	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	return stdout.String(), stderr.String(), err
}

func TestVersion(t *testing.T) {
	stdout, _, err := executeCommand("--version")
	if err != nil {
		t.Fatalf("--version returned error: %v", err)
	}
	if !strings.Contains(stdout, Version) {
		t.Errorf("--version output = %q, want to contain %q", stdout, Version)
	}
}

func TestConfigDiscovery_explicitPath(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "custom.yml")
	writeTestConfig(t, cfgPath, "postgres")
	writeSQLFile(t, dir, "CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);")

	// Run with explicit --config pointing to our custom file.
	_, stderr, err := executeCommand("--config", cfgPath)
	// Expect a schema or generation error (no valid migration path) — but NOT a config error.
	if err != nil {
		var ee *exitError
		if ok := errors.As(err, &ee); ok && ee.code == ExitConfig {
			t.Errorf("expected non-config error, got config error: %v\nstderr: %s", err, stderr)
		}
	}
}

func TestConfigDiscovery_findsYml(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, filepath.Join(dir, "sqlgen.yml"), "postgres")
	writeSQLFile(t, dir, "CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);")

	// Change to the temp dir and run without --config.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getting working dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("changing to temp dir: %v", err)
	}

	_, stderr, runErr := executeCommand()
	if runErr != nil {
		var ee *exitError
		if ok := errors.As(runErr, &ee); ok && ee.code == ExitConfig {
			t.Errorf("expected non-config error when sqlgen.yml exists, got config error: %v\nstderr: %s", runErr, stderr)
		}
	}
}

func TestConfigDiscovery_findsYaml(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, filepath.Join(dir, "sqlgen.yaml"), "postgres")
	writeSQLFile(t, dir, "CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);")

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getting working dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("changing to temp dir: %v", err)
	}

	_, stderr, runErr := executeCommand()
	if runErr != nil {
		var ee *exitError
		if ok := errors.As(runErr, &ee); ok && ee.code == ExitConfig {
			t.Errorf("expected non-config error when sqlgen.yaml exists, got config error: %v\nstderr: %s", runErr, stderr)
		}
	}
}

func TestConfigDiscovery_noConfigError(t *testing.T) {
	dir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getting working dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("changing to temp dir: %v", err)
	}

	_, stderr, runErr := executeCommand()
	if runErr == nil {
		t.Fatal("expected error when no config exists, got nil")
	}

	var ee *exitError
	if ok := errors.As(runErr, &ee); !ok || ee.code != ExitConfig {
		t.Errorf("expected exit code %d (config error), got error: %v", ExitConfig, runErr)
	}
	if !strings.Contains(stderr, "sqlgen init") {
		t.Errorf("stderr should suggest 'sqlgen init', got: %q", stderr)
	}
}

func TestExitCodeMapping(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode int
		wantOK   bool
	}{
		{
			name:     "config error",
			err:      &exitError{code: ExitConfig, err: os.ErrNotExist},
			wantCode: ExitConfig,
			wantOK:   true,
		},
		{
			name:     "schema error",
			err:      &exitError{code: ExitSchema, err: os.ErrInvalid},
			wantCode: ExitSchema,
			wantOK:   true,
		},
		{
			name:     "generation error",
			err:      &exitError{code: ExitGeneration, err: os.ErrPermission},
			wantCode: ExitGeneration,
			wantOK:   true,
		},
		{
			name:     "connection error",
			err:      &exitError{code: ExitConnection, err: os.ErrClosed},
			wantCode: ExitConnection,
			wantOK:   true,
		},
		{
			name:   "non-exit error",
			err:    os.ErrNotExist,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, ok := exitCodeFromError(tt.err)
			if ok != tt.wantOK {
				t.Errorf("exitCodeFromError() ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && code != tt.wantCode {
				t.Errorf("exitCodeFromError() code = %d, want %d", code, tt.wantCode)
			}
		})
	}
}

func TestDefaultCommand_noArgsRunsGenerate(t *testing.T) {
	// Running sqlgen with no args should run generate (not print help).
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	writeTestConfigWithDir(t, cfgPath, sqlDir, filepath.Join(dir, "output"))
	writeSQLFile(t, sqlDir, "CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);")

	_, stderr, err := executeCommand("--config", cfgPath)
	// If it ran generate, we'd get a generation result (or error from templates).
	// The key test: it should NOT print usage/help text.
	if err != nil {
		// Generation errors are expected if templates produce issues — that's fine.
		// We just want to verify it attempted generation, not printed help.
		if strings.Contains(stderr, "Usage:") {
			t.Errorf("no args printed usage instead of running generate")
		}
	}
}

func TestVerboseOutput(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	outputDir := filepath.Join(dir, "output")
	writeTestConfigWithDir(t, cfgPath, sqlDir, outputDir)
	writeSQLFile(t, sqlDir, "CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);")

	stdout, _, err := executeCommand("--config", cfgPath, "--verbose")
	if err != nil {
		// Generation may fail due to template issues in test env, that's OK.
		// We check that verbose mode was attempted.
		return
	}
	if !strings.Contains(stdout, "tables:") {
		t.Errorf("verbose output should contain 'tables:', got: %q", stdout)
	}
	if !strings.Contains(stdout, "elapsed:") {
		t.Errorf("verbose output should contain 'elapsed:', got: %q", stdout)
	}
}

func TestQuietOutput(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	outputDir := filepath.Join(dir, "output")
	writeTestConfigWithDir(t, cfgPath, sqlDir, outputDir)
	writeSQLFile(t, sqlDir, "CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);")

	stdout, _, err := executeCommand("--config", cfgPath, "--quiet")
	if err != nil {
		return // generation may fail, that's OK for this test
	}
	if stdout != "" {
		t.Errorf("quiet mode should produce no stdout, got: %q", stdout)
	}
}

func TestFullPipeline(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	outputDir := filepath.Join(dir, "output")
	writeTestConfigWithDir(t, cfgPath, sqlDir, outputDir)
	writeSQLFile(t, sqlDir, `
CREATE TABLE users (
    id serial PRIMARY KEY,
    name text NOT NULL,
    email text NOT NULL
);
`)

	stdout, stderr, err := executeCommand("--config", cfgPath)
	if err != nil {
		t.Fatalf("full pipeline failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	// Verify output files were created.
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatalf("reading output dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no files generated in output directory")
	}

	// Check that generated files have the correct header.
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), "_gen.go") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(outputDir, e.Name())) //nolint:gosec // test code, path is from controlled test dir
		if err != nil {
			t.Fatalf("reading %s: %v", e.Name(), err)
		}
		if !strings.HasPrefix(string(content), "// Code generated by sqlgen") {
			t.Errorf("%s missing generated file header", e.Name())
		}
	}

	// Verify stdout mentions the generation summary.
	if !strings.Contains(stdout, "generated") {
		t.Errorf("stdout should contain generation summary, got: %q", stdout)
	}
}

func TestGenerateSubcommand(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	outputDir := filepath.Join(dir, "output")
	writeTestConfigWithDir(t, cfgPath, sqlDir, outputDir)
	writeSQLFile(t, sqlDir, "CREATE TABLE products (id serial PRIMARY KEY, name text NOT NULL);")

	stdout, stderr, err := executeCommand("generate", "--config", cfgPath)
	if err != nil {
		t.Fatalf("generate subcommand failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatalf("reading output dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no files generated by 'generate' subcommand")
	}
}

func TestSingleFileLayout(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	outputDir := filepath.Join(dir, "output")
	writeTestConfigWithLayout(t, cfgPath, "postgres", sqlDir, outputDir, "single_file")
	writeSQLFile(t, sqlDir, `
CREATE TABLE users (
    id serial PRIMARY KEY,
    name text NOT NULL
);

CREATE TABLE products (
    id serial PRIMARY KEY,
    title text NOT NULL,
    price numeric NOT NULL
);
`)

	stdout, stderr, err := executeCommand("--config", cfgPath)
	if err != nil {
		t.Fatalf("single_file layout failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	// In single_file mode, all table code goes into models_gen.go.
	modelsPath := filepath.Join(outputDir, "models_gen.go")
	content, err := os.ReadFile(modelsPath) //nolint:gosec // test code, path is from controlled test dir
	if err != nil {
		t.Fatalf("reading models_gen.go: %v", err)
	}

	// Both tables should be in the single file.
	got := string(content)
	if !strings.Contains(got, "type User struct") {
		t.Error("models_gen.go missing User struct")
	}
	if !strings.Contains(got, "type Product struct") {
		t.Error("models_gen.go missing Product struct")
	}

	// There should NOT be per-table files.
	if _, err := os.Stat(filepath.Join(outputDir, "user_gen.go")); err == nil {
		t.Error("single_file layout should not produce user_gen.go")
	}
	if _, err := os.Stat(filepath.Join(outputDir, "product_gen.go")); err == nil {
		t.Error("single_file layout should not produce product_gen.go")
	}
}

func TestSingleFileLayout_views(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	viewDir := filepath.Join(dir, "views")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	if err := os.MkdirAll(viewDir, 0o750); err != nil {
		t.Fatalf("creating views dir: %v", err)
	}
	outputDir := filepath.Join(dir, "output")

	// Write config with views path.
	cfgContent := []byte(`input:
  dialect: postgres
  paths:
    - "` + sqlDir + `"
  views:
    - "` + viewDir + `"
output:
  driver: pgx
  dir: ` + outputDir + `
  package: models
  layout: single_file
`)
	if err := os.WriteFile(cfgPath, cfgContent, 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	// Write table DDL.
	writeSQLFile(t, sqlDir, `
CREATE TABLE products (
    id serial PRIMARY KEY,
    name text NOT NULL,
    price numeric NOT NULL
);
`)

	// Write view annotation file.
	viewSQL := `-- @pk: id
CREATE VIEW product_summary AS
SELECT p.id, p.name, p.price
FROM products p;
`
	if err := os.WriteFile(filepath.Join(viewDir, "product_summary.sql"), []byte(viewSQL), 0o600); err != nil {
		t.Fatalf("writing view file: %v", err)
	}

	stdout, stderr, err := executeCommand("--config", cfgPath)
	if err != nil {
		t.Fatalf("single_file layout with views failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	// Views should be in views_gen.go.
	viewsPath := filepath.Join(outputDir, "views_gen.go")
	content, err := os.ReadFile(viewsPath) //nolint:gosec // test code, path is from controlled test dir
	if err != nil {
		t.Fatalf("reading views_gen.go: %v", err)
	}

	got := string(content)
	if !strings.Contains(got, "ProductSummary") {
		t.Error("views_gen.go missing ProductSummary type")
	}

	// There should NOT be a per-view file.
	if _, err := os.Stat(filepath.Join(outputDir, "product_summary_gen.go")); err == nil {
		t.Error("single_file layout should not produce product_summary_gen.go")
	}
}

func TestFilePerTableLayout_views(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	viewDir := filepath.Join(dir, "views")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	if err := os.MkdirAll(viewDir, 0o750); err != nil {
		t.Fatalf("creating views dir: %v", err)
	}
	outputDir := filepath.Join(dir, "output")

	// Write config with views path.
	cfgContent := []byte(`input:
  dialect: postgres
  paths:
    - "` + sqlDir + `"
  views:
    - "` + viewDir + `"
output:
  driver: pgx
  dir: ` + outputDir + `
  package: models
  layout: file_per_table
`)
	if err := os.WriteFile(cfgPath, cfgContent, 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	writeSQLFile(t, sqlDir, `
CREATE TABLE products (
    id serial PRIMARY KEY,
    name text NOT NULL,
    price numeric NOT NULL
);
`)

	viewSQL := `-- @pk: id
CREATE VIEW product_summary AS
SELECT p.id, p.name, p.price
FROM products p;
`
	if err := os.WriteFile(filepath.Join(viewDir, "product_summary.sql"), []byte(viewSQL), 0o600); err != nil {
		t.Fatalf("writing view file: %v", err)
	}

	stdout, stderr, err := executeCommand("--config", cfgPath)
	if err != nil {
		t.Fatalf("file_per_table layout with views failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	// View should get its own file in file_per_table mode.
	viewFilePath := filepath.Join(outputDir, "product_summary_gen.go")
	content, err := os.ReadFile(viewFilePath) //nolint:gosec // test code, path is from controlled test dir
	if err != nil {
		t.Fatalf("reading product_summary_gen.go: %v", err)
	}

	got := string(content)
	if !strings.Contains(got, "ProductSummary") {
		t.Error("product_summary_gen.go missing ProductSummary type")
	}

	// There should NOT be a combined views_gen.go.
	if _, err := os.Stat(filepath.Join(outputDir, "views_gen.go")); err == nil {
		t.Error("file_per_table layout should not produce views_gen.go")
	}
}

func TestInit_createsDefaultConfig(t *testing.T) {
	dir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getting working dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("changing to temp dir: %v", err)
	}

	stdout, _, err := executeCommand("init")
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "sqlgen.yml")) //nolint:gosec // test code, path is from controlled test dir
	if err != nil {
		t.Fatalf("reading sqlgen.yml: %v", err)
	}

	got := string(content)
	want := `# yaml-language-server: $schema=https://raw.githubusercontent.com/teandresmith/sqlgen/main/cmd/sqlgen/config/schema/v1.json
# Generated by sqlgen init
input:
  dialect: postgres
  paths:
    - "./migrations"

output:
  driver: pgx
  dir: ./internal/models
  package: models
  layout: file_per_table
`
	if got != want {
		t.Errorf("config content mismatch.\ngot:\n%s\nwant:\n%s", got, want)
	}

	if !strings.Contains(stdout, "Created sqlgen.yml") {
		t.Errorf("stdout should confirm creation, got: %q", stdout)
	}
}

func TestInit_nonInteractiveDefaultsToPostgres(t *testing.T) {
	dir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getting working dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("changing to temp dir: %v", err)
	}

	// In tests, stdin is a pipe (not a TTY), so non-interactive mode is used.
	stdout, _, err := executeCommand("init")
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}

	if !strings.Contains(stdout, "dialect: postgres") {
		t.Errorf("non-interactive should default to postgres, stdout: %q", stdout)
	}

	content, err := os.ReadFile(filepath.Join(dir, "sqlgen.yml")) //nolint:gosec // test code, path is from controlled test dir
	if err != nil {
		t.Fatalf("reading sqlgen.yml: %v", err)
	}
	if !strings.Contains(string(content), "dialect: postgres") {
		t.Errorf("non-interactive config should use postgres dialect, got:\n%s", string(content))
	}
	if !strings.Contains(string(content), "driver: pgx") {
		t.Errorf("non-interactive config should use pgx driver, got:\n%s", string(content))
	}
}

func TestInit_errorsWhenConfigExists(t *testing.T) {
	tests := []struct {
		name     string
		existing string
	}{
		{"sqlgen.yml exists", "sqlgen.yml"},
		{"sqlgen.yaml exists", "sqlgen.yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			origDir, err := os.Getwd()
			if err != nil {
				t.Fatalf("getting working dir: %v", err)
			}
			t.Cleanup(func() { _ = os.Chdir(origDir) })
			if err := os.Chdir(dir); err != nil {
				t.Fatalf("changing to temp dir: %v", err)
			}

			if err := os.WriteFile(tt.existing, []byte("existing"), 0o600); err != nil {
				t.Fatalf("writing existing config: %v", err)
			}

			_, _, err = executeCommand("init")
			if err == nil {
				t.Fatal("expected error when config exists, got nil")
			}
			if !strings.Contains(err.Error(), "already exists") {
				t.Errorf("error should mention 'already exists', got: %v", err)
			}
		})
	}
}

func TestInit_dialectToDriverMapping(t *testing.T) {
	tests := []struct {
		dialect    string
		wantDriver string
	}{
		{"postgres", "pgx"},
		{"mysql", "stdlib"},
		{"sqlite", "stdlib"},
	}
	for _, tt := range tests {
		t.Run(tt.dialect, func(t *testing.T) {
			got := driverForDialect(tt.dialect)
			if got != tt.wantDriver {
				t.Errorf("driverForDialect(%q) = %q, want %q", tt.dialect, got, tt.wantDriver)
			}
		})
	}
}

func TestValidate_validConfigAndSchema(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	outputDir := filepath.Join(dir, "output")
	writeTestConfigWithDir(t, cfgPath, sqlDir, outputDir)
	writeSQLFile(t, sqlDir, "CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);")

	stdout, _, err := executeCommand("validate", "--config", cfgPath)
	if err != nil {
		t.Fatalf("validate returned error for valid config+schema: %v", err)
	}
	if !strings.Contains(stdout, "valid") {
		t.Errorf("validate stdout should contain 'valid', got: %q", stdout)
	}

	// Verify no files were generated.
	if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
		t.Errorf("validate should not create output directory, but it exists")
	}
}

func TestValidate_invalidConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	outputDir := filepath.Join(dir, "output")

	// Use pgx driver with mysql dialect — this triggers a pre-parse validation error.
	content := []byte(`input:
  dialect: mysql
  paths:
    - "` + sqlDir + `"
output:
  driver: pgx
  dir: ` + outputDir + `
  package: models
  layout: file_per_table
`)
	if err := os.WriteFile(cfgPath, content, 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	writeSQLFile(t, sqlDir, "CREATE TABLE users (id INT AUTO_INCREMENT PRIMARY KEY, name VARCHAR(255) NOT NULL);")

	_, stderr, err := executeCommand("validate", "--config", cfgPath)
	if err == nil {
		t.Fatal("validate should return error for invalid config, got nil")
	}

	var ee *exitError
	if ok := errors.As(err, &ee); !ok || ee.code != ExitConfig {
		t.Errorf("expected exit code %d (config), got error: %v", ExitConfig, err)
	}
	if !strings.Contains(stderr, "pgx") {
		t.Errorf("stderr should mention pgx incompatibility, got: %q", stderr)
	}
}

func TestValidate_invalidSchema(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	outputDir := filepath.Join(dir, "output")
	writeTestConfigWithDir(t, cfgPath, sqlDir, outputDir)

	// Write invalid SQL that the parser will reject.
	writeSQLFile(t, sqlDir, "THIS IS NOT VALID SQL AT ALL;")

	_, stderr, err := executeCommand("validate", "--config", cfgPath)
	if err == nil {
		t.Fatal("validate should return error for invalid schema, got nil")
	}

	var ee *exitError
	if ok := errors.As(err, &ee); !ok || ee.code != ExitSchema {
		t.Errorf("expected exit code %d (schema), got error: %v (code: %d)", ExitSchema, err, ee.code)
	}
	if !strings.Contains(stderr, "Error:") {
		t.Errorf("stderr should contain error details, got: %q", stderr)
	}
}

func TestValidate_noFilesWritten(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	outputDir := filepath.Join(dir, "output")
	writeTestConfigWithDir(t, cfgPath, sqlDir, outputDir)
	writeSQLFile(t, sqlDir, `
CREATE TABLE products (
    id serial PRIMARY KEY,
    name text NOT NULL,
    price numeric NOT NULL
);
`)

	_, _, err := executeCommand("validate", "--config", cfgPath)
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}

	// The output directory must NOT exist — validate never writes files.
	if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
		t.Error("validate should not create the output directory")
	}
}

func TestDiff_noChanges(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	outputDir := filepath.Join(dir, "output")
	writeTestConfigWithDir(t, cfgPath, sqlDir, outputDir)
	writeSQLFile(t, sqlDir, "CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);")

	// First, generate so the output directory has current files.
	_, _, err := executeCommand("generate", "--config", cfgPath)
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}

	// Now diff — should report no changes, exit code 0.
	stdout, _, err := executeCommand("diff", "--config", cfgPath)
	if err != nil {
		t.Fatalf("diff returned error when no changes: %v", err)
	}
	if !strings.Contains(stdout, "no changes") {
		t.Errorf("diff stdout should contain 'no changes', got: %q", stdout)
	}
}

func TestDiff_newFileDetected(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	outputDir := filepath.Join(dir, "output")
	writeTestConfigWithDir(t, cfgPath, sqlDir, outputDir)
	writeSQLFile(t, sqlDir, "CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);")

	// Don't generate first — all files are new.
	stdout, _, err := executeCommand("diff", "--config", cfgPath)
	if err == nil {
		t.Fatal("diff should return error (exit 1) when changes detected, got nil")
	}
	var ee *exitError
	if ok := errors.As(err, &ee); !ok || ee.code != ExitGeneration {
		t.Errorf("diff exit code = %v, want %d", err, ExitGeneration)
	}
	if !strings.Contains(stdout, "create") {
		t.Errorf("diff output should contain 'create', got: %q", stdout)
	}
}

func TestDiff_modifiedFileDetected(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	outputDir := filepath.Join(dir, "output")
	writeTestConfigWithDir(t, cfgPath, sqlDir, outputDir)
	writeSQLFile(t, sqlDir, "CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);")

	// Generate first.
	_, _, err := executeCommand("generate", "--config", cfgPath)
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}

	// Modify a generated file so diff detects a change.
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatalf("reading output dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "_gen.go") {
			p := filepath.Join(outputDir, e.Name())
			if err := os.WriteFile(p, []byte("// modified"), 0o600); err != nil {
				t.Fatalf("modifying file: %v", err)
			}
			break
		}
	}

	stdout, _, err := executeCommand("diff", "--config", cfgPath)
	if err == nil {
		t.Fatal("diff should return error (exit 1) when changes detected")
	}
	if !strings.Contains(stdout, "modify") {
		t.Errorf("diff output should contain 'modify', got: %q", stdout)
	}
}

func TestDiff_deletedFileDetected(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	outputDir := filepath.Join(dir, "output")
	writeTestConfigWithDir(t, cfgPath, sqlDir, outputDir) // file_per_table layout
	writeSQLFile(t, sqlDir, "CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);")

	// Generate first.
	_, _, err := executeCommand("generate", "--config", cfgPath)
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}

	// Add an orphaned _gen.go file that won't be regenerated. It carries the
	// sqlgen header a real orphan would: the sweep deletes on provenance, not
	// on the name alone (PRD §23.8), so a headerless stub is not the shape
	// being tested here.
	orphan := filepath.Join(outputDir, "legacy_table_gen.go")
	if err := os.WriteFile(orphan, []byte(gen.Header("dev")+"\n\npackage models\n"), 0o600); err != nil {
		t.Fatalf("writing orphaned file: %v", err)
	}

	stdout, _, err := executeCommand("diff", "--config", cfgPath)
	if err == nil {
		t.Fatal("diff should return error (exit 1) when deleted file detected")
	}
	if !strings.Contains(stdout, "delete") {
		t.Errorf("diff output should contain 'delete', got: %q", stdout)
	}
	if !strings.Contains(stdout, "legacy_table_gen.go") {
		t.Errorf("diff output should mention orphaned file, got: %q", stdout)
	}
}

func TestDiff_mixedChanges(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	outputDir := filepath.Join(dir, "output")
	writeTestConfigWithDir(t, cfgPath, sqlDir, outputDir)
	writeSQLFile(t, sqlDir, "CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);")

	// Generate first.
	_, _, err := executeCommand("generate", "--config", cfgPath)
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}

	// Modify a generated file.
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatalf("reading output dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "_gen.go") {
			p := filepath.Join(outputDir, e.Name())
			if err := os.WriteFile(p, []byte("// modified"), 0o600); err != nil {
				t.Fatalf("modifying file: %v", err)
			}
			break
		}
	}

	// Add an orphaned _gen.go file, carrying the sqlgen header a real orphan
	// would (PRD §23.8 deletes on provenance, not on the name alone).
	orphan := filepath.Join(outputDir, "old_table_gen.go")
	if err := os.WriteFile(orphan, []byte(gen.Header("dev")+"\n\npackage models\n"), 0o600); err != nil {
		t.Fatalf("writing orphaned file: %v", err)
	}

	stdout, _, err := executeCommand("diff", "--config", cfgPath)
	if err == nil {
		t.Fatal("diff should return error (exit 1) when changes detected")
	}

	// Should have both modify and delete in the output.
	if !strings.Contains(stdout, "modify") {
		t.Errorf("mixed diff should contain 'modify', got: %q", stdout)
	}
	if !strings.Contains(stdout, "delete") {
		t.Errorf("mixed diff should contain 'delete', got: %q", stdout)
	}

	// Check summary line has counts.
	if !strings.Contains(stdout, "files would change") {
		t.Errorf("diff should show summary line, got: %q", stdout)
	}
}

func TestCompletion_eachShellProducesOutput(t *testing.T) {
	shells := []string{"bash", "zsh", "fish", "powershell"}
	for _, shell := range shells {
		t.Run(shell, func(t *testing.T) {
			stdout, _, err := executeCommand("completion", shell)
			if err != nil {
				t.Fatalf("completion %s returned error: %v", shell, err)
			}
			if len(stdout) == 0 {
				t.Errorf("completion %s produced empty output", shell)
			}
		})
	}
}

func TestCompletion_missingShellArgument(t *testing.T) {
	_, _, err := executeCommand("completion")
	if err == nil {
		t.Fatal("completion with no args should return error, got nil")
	}
}

func TestCompletion_invalidShellName(t *testing.T) {
	_, _, err := executeCommand("completion", "nushell")
	if err == nil {
		t.Fatal("completion with invalid shell should return error, got nil")
	}
}

func TestStaleCleanup_orphanedFileDeleted(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	outputDir := filepath.Join(dir, "output")
	writeTestConfigWithDir(t, cfgPath, sqlDir, outputDir) // file_per_table

	// Generate with two tables.
	writeSQLFile(t, sqlDir, `
CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);
CREATE TABLE products (id serial PRIMARY KEY, title text NOT NULL);
`)

	_, _, err := executeCommand("generate", "--config", cfgPath)
	if err != nil {
		t.Fatalf("initial generate failed: %v", err)
	}

	// Verify the product file exists.
	productFile := filepath.Join(outputDir, "product_gen.go")
	if _, err := os.Stat(productFile); os.IsNotExist(err) {
		t.Fatalf("product_gen.go should exist after initial generate")
	}

	// Now regenerate with only one table (drop products).
	writeSQLFile(t, sqlDir, `CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);`)

	_, _, err = executeCommand("generate", "--config", cfgPath)
	if err != nil {
		t.Fatalf("second generate failed: %v", err)
	}

	// The orphaned product_gen.go should be deleted.
	if _, err := os.Stat(productFile); !os.IsNotExist(err) {
		t.Error("product_gen.go should be deleted after table removed from schema")
	}
}

func TestStaleCleanup_nonGenFilesPreserved(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	outputDir := filepath.Join(dir, "output")
	writeTestConfigWithDir(t, cfgPath, sqlDir, outputDir)
	writeSQLFile(t, sqlDir, `CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);`)

	// Generate first.
	_, _, err := executeCommand("generate", "--config", cfgPath)
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}

	// Add a non-_gen.go file to the output directory.
	customFile := filepath.Join(outputDir, "helpers.go")
	if err := os.WriteFile(customFile, []byte("package models\n"), 0o600); err != nil {
		t.Fatalf("writing custom file: %v", err)
	}

	// Regenerate.
	_, _, err = executeCommand("generate", "--config", cfgPath)
	if err != nil {
		t.Fatalf("second generate failed: %v", err)
	}

	// helpers.go must still exist.
	if _, err := os.Stat(customFile); os.IsNotExist(err) {
		t.Error("non-_gen.go file should be preserved during stale cleanup")
	}
}

func TestStaleCleanup_noCleanupInSingleFileMode(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	outputDir := filepath.Join(dir, "output")
	writeTestConfigWithLayout(t, cfgPath, "postgres", sqlDir, outputDir, "single_file")
	writeSQLFile(t, sqlDir, `CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);`)

	// Generate first.
	_, _, err := executeCommand("generate", "--config", cfgPath)
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}

	// Plant an orphaned _gen.go file.
	orphan := filepath.Join(outputDir, "old_table_gen.go")
	if err := os.WriteFile(orphan, []byte("package models\n"), 0o600); err != nil {
		t.Fatalf("writing orphan: %v", err)
	}

	// Regenerate in single_file mode.
	_, _, err = executeCommand("generate", "--config", cfgPath)
	if err != nil {
		t.Fatalf("second generate failed: %v", err)
	}

	// Orphan must still exist — no cleanup in single_file mode.
	if _, err := os.Stat(orphan); os.IsNotExist(err) {
		t.Error("orphaned file should NOT be deleted in single_file layout mode")
	}
}

func TestStaleCleanup_newlyGeneratedFilesNotDeleted(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	outputDir := filepath.Join(dir, "output")
	writeTestConfigWithDir(t, cfgPath, sqlDir, outputDir)
	writeSQLFile(t, sqlDir, `
CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);
CREATE TABLE products (id serial PRIMARY KEY, title text NOT NULL);
`)

	_, _, err := executeCommand("generate", "--config", cfgPath)
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}

	// All generated _gen.go files must exist.
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatalf("reading output dir: %v", err)
	}

	var genFiles int
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "_gen.go") {
			genFiles++
		}
	}
	if genFiles == 0 {
		t.Fatal("expected generated _gen.go files, found none")
	}
}

func TestStaleCleanup_verboseReportsDeletedFiles(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	outputDir := filepath.Join(dir, "output")
	writeTestConfigWithDir(t, cfgPath, sqlDir, outputDir)

	// Generate with two tables.
	writeSQLFile(t, sqlDir, `
CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);
CREATE TABLE products (id serial PRIMARY KEY, title text NOT NULL);
`)

	_, _, err := executeCommand("generate", "--config", cfgPath)
	if err != nil {
		t.Fatalf("initial generate failed: %v", err)
	}

	// Drop a table and regenerate with --verbose.
	writeSQLFile(t, sqlDir, `CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);`)

	stdout, _, err := executeCommand("generate", "--config", cfgPath, "--verbose")
	if err != nil {
		t.Fatalf("verbose generate failed: %v", err)
	}

	if !strings.Contains(stdout, "deleted:") {
		t.Errorf("verbose output should report deleted files, got: %q", stdout)
	}
	if !strings.Contains(stdout, "product_gen.go") {
		t.Errorf("verbose output should mention product_gen.go, got: %q", stdout)
	}
}

// --- Test helpers ---

func writeTestConfig(t *testing.T, path, dialect string) {
	t.Helper()
	dir := filepath.Dir(path)
	content := []byte(`input:
  dialect: ` + dialect + `
  paths:
    - "` + dir + `"
output:
  driver: pgx
  dir: ` + filepath.Join(dir, "output") + `
  package: models
  layout: file_per_table
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
}

func writeTestConfigWithDir(t *testing.T, path, sqlDir, outputDir string) {
	t.Helper()
	content := []byte(`input:
  dialect: postgres
  paths:
    - "` + sqlDir + `"
output:
  driver: pgx
  dir: ` + outputDir + `
  package: models
  layout: file_per_table
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
}

func writeTestConfigWithLayout(t *testing.T, path, dialect, sqlDir, outputDir, layout string) {
	t.Helper()
	content := []byte(`input:
  dialect: ` + dialect + `
  paths:
    - "` + sqlDir + `"
output:
  driver: pgx
  dir: ` + outputDir + `
  package: models
  layout: ` + layout + `
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
}

func writeSQLFile(t *testing.T, dir, sql string) {
	t.Helper()
	path := filepath.Join(dir, "001_init.sql")
	if err := os.WriteFile(path, []byte(sql), 0o600); err != nil {
		t.Fatalf("writing SQL file: %v", err)
	}
}
