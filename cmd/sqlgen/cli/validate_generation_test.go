package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidate_reachesGenerationPhase pins that validate runs generation-phase
// checks.
// `validate` used to stop after ValidatePostParse, so a config that only
// `generate` rejects printed "config and schema are valid" and then failed on
// the next generate. Both shapes of the resolved-field-name rule must now be
// visible from validate: a rename onto a name the templates already declare,
// and a plain column whose derived name does the same.
func TestValidate_reachesGenerationPhase(t *testing.T) {
	tests := []struct {
		name    string
		schema  string
		table   string
		wantErr string
	}{
		{
			name:    "renamed onto a reserved name",
			schema:  "CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL);",
			table:   "\ntables:\n  users:\n    column_map:\n      name:\n        name: And\n",
			wantErr: `resolves to Go field name "And"`,
		},
		{
			name:    "plain column name is reserved",
			schema:  "CREATE TABLE users (id serial PRIMARY KEY, columns text NOT NULL);",
			wantErr: `resolves to Go field name "Columns"`,
		},
		{
			name:    "duplicate resolved name",
			schema:  "CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL, label text NOT NULL);",
			table:   "\ntables:\n  users:\n    column_map:\n      label:\n        name: Name\n",
			wantErr: `is claimed by both`,
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
			outputDir := filepath.Join(dir, "output")
			writeValidateConfig(t, cfgPath, sqlDir, outputDir, tt.table)
			writeSQLFile(t, sqlDir, tt.schema)

			stdout, stderr, err := executeCommand("validate", "--config", cfgPath)
			if err == nil {
				t.Fatalf("validate returned nil error; stdout = %q", stdout)
			}

			var ee *exitError
			if !errors.As(err, &ee) || ee.code != ExitConfig {
				t.Errorf("expected exit code %d (config), got error: %v", ExitConfig, err)
			}
			if !strings.Contains(stderr, tt.wantErr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, tt.wantErr)
			}
			if strings.Contains(stdout, "config and schema are valid") {
				t.Error("validate reported the config valid while generate would reject it")
			}
			// The generation-phase check must not turn validate into a
			// generator: nothing is written.
			if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
				t.Error("validate created the output directory")
			}
		})
	}
}

// writeValidateConfig writes the standard postgres test config with an extra
// YAML section appended, so a case can add the `tables:` block it needs.
func writeValidateConfig(t *testing.T, path, sqlDir, outputDir, extra string) {
	t.Helper()
	content := `input:
  dialect: postgres
  paths:
    - "` + sqlDir + `"
output:
  driver: pgx
  dir: ` + outputDir + `
  package: models
  layout: file_per_table
` + extra
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
}

// validate builds the generation contexts even after the config rules fail,
// so a relationship with no `name:` (required, PRD §4.8) must yield only the
// config error: no panic for an O2O edge, and no api-context error for an
// O2M edge's filter member.
func TestValidate_relationshipWithoutName(t *testing.T) {
	const schema = `CREATE TABLE owners (id serial PRIMARY KEY);
CREATE TABLE owner_profiles (id serial PRIMARY KEY, owner_id int NOT NULL UNIQUE REFERENCES owners(id), bio text);
CREATE TABLE tags (id serial PRIMARY KEY);
CREATE TABLE owner_tags (owner_id int NOT NULL REFERENCES owners(id), tag_id int NOT NULL REFERENCES tags(id), PRIMARY KEY (owner_id, tag_id));`
	tests := []struct {
		name string
		rel  string
	}{
		{name: "o2o", rel: "type: o2o\n        table: owner_profiles\n        fk: owner_id\n"},
		{name: "o2m", rel: "type: o2m\n        table: owner_profiles\n        fk: owner_id\n        filter: \"bio IS NOT NULL\"\n"},
		{name: "m2m", rel: "type: m2m\n        table: tags\n        junction: owner_tags\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "sqlgen.yml")
			sqlDir := filepath.Join(dir, "migrations")
			if err := os.MkdirAll(sqlDir, 0o750); err != nil {
				t.Fatalf("creating migrations dir: %v", err)
			}
			// The api block builds the filter members a nameless edge would
			// render only half of.
			writeValidateConfig(t, cfgPath, sqlDir, filepath.Join(dir, "output"),
				"api:\n  enabled: true\n  graphql:\n    enabled: true\n    schema_dir: ./graph\n    resolver_dir: ./graph\n    package: graph\n"+
					"tables:\n  owners:\n    relationships:\n      - "+tt.rel)
			writeSQLFile(t, sqlDir, schema)

			_, stderr, err := executeCommand("validate", "--config", cfgPath)
			var ee *exitError
			if !errors.As(err, &ee) || ee.code != ExitConfig {
				t.Fatalf("validate error = %v, want exit code %d (config)", err, ExitConfig)
			}
			const want = "Error: tables.owners.relationships[0].name: required"
			if got := strings.Count(stderr, "Error: "); got != 1 || !strings.Contains(stderr, want) {
				t.Errorf("stderr = %q, want the one error %q", stderr, want)
			}
		})
	}
}

const softDeleteMismatchErr = `soft_delete_columns: column "zz_bad" in table public.zz_c has incompatible type "varchar(10)"`

// writeSoftDeleteMismatchProject writes a project whose soft delete column has
// a SQL type its configured type does not accept, and returns the config path
// and the output dir.
func writeSoftDeleteMismatchProject(t *testing.T) (cfgPath, outputDir string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath = filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	outputDir = filepath.Join(dir, "output")
	writeValidateConfig(t, cfgPath, sqlDir, outputDir,
		"generation:\n  soft_delete_columns:\n    - name: zz_bad\n      type: integer\n")
	writeSQLFile(t, sqlDir, "CREATE TABLE zz_c (id serial PRIMARY KEY, zz_bad varchar(10) NOT NULL);")
	return cfgPath, outputDir
}

// A soft delete column whose SQL type does not fit its configured type is one
// config error. Generation does not check the type a second time, so validate,
// which builds the contexts after the config rules fail, reports it once.
func TestValidate_softDeleteTypeMismatchReportedOnce(t *testing.T) {
	cfgPath, _ := writeSoftDeleteMismatchProject(t)

	_, stderr, err := executeCommand("validate", "--config", cfgPath)
	if ee, ok := errors.AsType[*exitError](err); !ok || ee.code != ExitConfig {
		t.Fatalf("validate error = %v, want exit code %d (config)", err, ExitConfig)
	}
	if got := strings.Count(stderr, "Error: "); got != 1 || !strings.Contains(stderr, "Error: "+softDeleteMismatchErr) {
		t.Errorf("stderr = %q, want the one error %q", stderr, softDeleteMismatchErr)
	}
}

// A NOT NULL timestamp soft delete column stops generate at validation, before
// funcSoftDeleteFilterOverride's fallback emits a Null field into the
// non-nullable comparator.Time, which does not compile.
func TestGenerate_notNullTimestampSoftDeleteRejected(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sqlgen.yml")
	sqlDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(sqlDir, 0o750); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}
	outputDir := filepath.Join(dir, "output")
	writeValidateConfig(t, cfgPath, sqlDir, outputDir,
		"generation:\n  soft_delete_columns:\n    - name: zz_ts\n      type: timestamp\n")
	writeSQLFile(t, sqlDir, "CREATE TABLE zz_c (id serial PRIMARY KEY, zz_ts timestamptz NOT NULL DEFAULT now());")

	_, stderr, err := executeCommand("generate", "--config", cfgPath)
	if ee, ok := errors.AsType[*exitError](err); !ok || ee.code != ExitConfig {
		t.Fatalf("generate error = %v, want exit code %d (config)", err, ExitConfig)
	}
	const want = `Post-parse validation error: soft_delete_columns: column "zz_ts" in table public.zz_c is NOT NULL`
	if !strings.Contains(stderr, want) {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
		t.Error("generate wrote the output directory")
	}
}

// Post-parse validation is the only soft delete type check, so every command
// that builds generation contexts must run it first and stop there.
func TestSoftDeleteTypeMismatchStopsEveryContextBuilder(t *testing.T) {
	for _, args := range [][]string{{"generate"}, {"diff"}, {"lint"}, {"graphql", "gen"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			cfgPath, outputDir := writeSoftDeleteMismatchProject(t)

			_, stderr, err := executeCommand(append(args, "--config", cfgPath)...)
			if ee, ok := errors.AsType[*exitError](err); !ok || ee.code != ExitConfig {
				t.Fatalf("error = %v, want exit code %d (config)", err, ExitConfig)
			}
			if want := "Post-parse validation error: " + softDeleteMismatchErr; !strings.Contains(stderr, want) {
				t.Errorf("stderr = %q, want %q", stderr, want)
			}
			if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
				t.Error("the command wrote the output directory")
			}
		})
	}
}
