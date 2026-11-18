package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fccRunGenerate writes schemaSQL and a config into a temp dir, runs
// `sqlgen generate` through the root command, and returns the output dir and
// the command's stdout and stderr. inputExtra is indented under `input:`;
// rootExtra is appended at the root. The command must succeed.
func fccRunGenerate(t *testing.T, dialect, driver, schemaSQL, inputExtra, rootExtra string) (outDir, stdout, stderr string) {
	t.Helper()
	dir := t.TempDir()
	outDir = filepath.Join(dir, "models")
	schemaPath := filepath.Join(dir, "schema.sql")
	if err := os.WriteFile(schemaPath, []byte(schemaSQL), 0o600); err != nil {
		t.Fatalf("writing schema: %v", err)
	}
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	body := "version: v1\ninput:\n  dialect: " + dialect + "\n  paths:\n    - " + schemaPath + "\n" + inputExtra +
		"output:\n  driver: " + driver + "\n  dir: " + outDir + "\n  package: models\n" + rootExtra
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	stdout, stderr, err := executeCommand("generate", "--config", cfgPath)
	if err != nil {
		t.Fatalf("sqlgen generate: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	return outDir, stdout, stderr
}

// TestGenerateCommand_KeyPrefixWarningOnStderr pins that `sqlgen generate`
// prints the defaulted-key_prefix warning to stderr when `cache.key_prefix`
// is unset, and prints nothing of the kind when it is set.
func TestGenerateCommand_KeyPrefixWarningOnStderr(t *testing.T) {
	const schemaSQL = "CREATE TABLE products (id bigserial PRIMARY KEY, name text NOT NULL);\n"
	const warning = `Warning: sqlgen: cache.key_prefix unset — using default "sqlgen". Set cache.key_prefix explicitly to silence this warning.` + "\n"
	tests := []struct {
		name     string
		cache    string
		wantWarn bool
	}{
		{name: "unset key_prefix warns on stderr", cache: "cache:\n  enabled: true\n", wantWarn: true},
		{name: "explicit key_prefix prints no warning", cache: "cache:\n  enabled: true\n  key_prefix: app\n", wantWarn: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, stdout, stderr := fccRunGenerate(t, "postgres", "pgx", schemaSQL, "", tt.cache)
			if got := strings.Contains(stderr, warning); got != tt.wantWarn {
				t.Errorf("stderr carries the key_prefix warning = %v, want %v\nstderr: %s", got, tt.wantWarn, stderr)
			}
			if strings.Contains(stdout, "cache.key_prefix unset") {
				t.Errorf("stdout carries the key_prefix warning, want it on stderr only\nstdout: %s", stdout)
			}
		})
	}
}

// TestGenerateCommand_InputSchemaIgnoredWithoutSchemas pins that an
// `input.schema` other than `*` is ignored on MySQL and SQLite: the table
// keeps its bare name. PostgreSQL, which honors it, is the control.
func TestGenerateCommand_InputSchemaIgnoredWithoutSchemas(t *testing.T) {
	tests := []struct {
		name      string
		dialect   string
		driver    string
		schemaSQL string
		want      string
	}{
		{
			name:      "mysql keeps the bare table name",
			dialect:   "mysql",
			driver:    "stdlib",
			schemaSQL: "CREATE TABLE products (id BIGINT AUTO_INCREMENT PRIMARY KEY, name VARCHAR(255) NOT NULL);\n",
			want:      `TableProducts hook.TableName = "products"`,
		},
		{
			name:      "sqlite keeps the bare table name",
			dialect:   "sqlite",
			driver:    "stdlib",
			schemaSQL: "CREATE TABLE products (id INTEGER PRIMARY KEY, name TEXT NOT NULL);\n",
			want:      `TableProducts hook.TableName = "products"`,
		},
		{
			name:      "postgres assigns the input schema",
			dialect:   "postgres",
			driver:    "pgx",
			schemaSQL: "CREATE SCHEMA app;\nCREATE TABLE products (id bigserial PRIMARY KEY, name text NOT NULL);\n",
			want:      `TableProducts hook.TableName = "app.products"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outDir, _, _ := fccRunGenerate(t, tt.dialect, tt.driver, tt.schemaSQL, "  schema: app\n", "")
			body := readGenerated(t, outDir, "tablenames_gen.go")
			if !strings.Contains(body, tt.want) {
				t.Errorf("tablenames_gen.go missing %q\n%s", tt.want, body)
			}
		})
	}
}
