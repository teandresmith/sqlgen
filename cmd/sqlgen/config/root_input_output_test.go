package config_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// tdfLoad writes content as sqlgen.yml and loads it, failing the test on a
// load error.
func tdfLoad(t *testing.T, content string) *config.RootConfig {
	t.Helper()
	cfg, err := config.LoadConfig(writeTestConfig(t, content))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	return cfg
}

// tdfReadPRD returns docs/PRD.md.
func tdfReadPRD(t *testing.T) string {
	t.Helper()
	prd, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "PRD.md"))
	if err != nil {
		t.Fatalf("ReadFile(docs/PRD.md) = %v, want nil", err)
	}
	return string(prd)
}

// tdfPRDYAMLBlock returns the first fenced yaml block after heading in prd.
func tdfPRDYAMLBlock(t *testing.T, prd, heading string) string {
	t.Helper()
	_, after, found := strings.Cut(prd, "\n"+heading+"\n")
	if !found {
		t.Fatalf("heading %q not found in docs/PRD.md", heading)
	}
	_, after, found = strings.Cut(after, "```yaml\n")
	if !found {
		t.Fatalf("no fenced yaml block under %q", heading)
	}
	block, _, found := strings.Cut(after, "\n```")
	if !found {
		t.Fatalf("unterminated yaml block under %q", heading)
	}
	return block + "\n"
}

// TestLoadConfig_RootMapsDefaultToEmpty pins the §4.1 defaults of the
// per-entity maps: a config that never writes tables, views or extras loads
// each as an empty, non-nil map.
func TestLoadConfig_RootMapsDefaultToEmpty(t *testing.T) {
	cfg := tdfLoad(t, ldBase)
	tests := []struct {
		name   string
		isNil  bool
		length int
	}{
		{name: "tables", isNil: cfg.Tables == nil, length: len(cfg.Tables)},
		{name: "views", isNil: cfg.Views == nil, length: len(cfg.Views)},
		{name: "extras", isNil: cfg.Extras == nil, length: len(cfg.Extras)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.isNil || tt.length != 0 {
				t.Errorf("LoadConfig() %s: nil = %t, len = %d; want an empty non-nil map", tt.name, tt.isNil, tt.length)
			}
		})
	}
}

// TestLoadConfig_InputDefaults pins the §4.2 defaults of input.dialect,
// input.paths and input.views when the input block leaves them unset.
func TestLoadConfig_InputDefaults(t *testing.T) {
	cfg := tdfLoad(t, "input:\n  source: files\noutput:\n  dir: ./models\n")

	t.Run("dialect defaults to postgres", func(t *testing.T) {
		if cfg.Input.Dialect != "postgres" {
			t.Errorf("LoadConfig() input.dialect = %q, want %q", cfg.Input.Dialect, "postgres")
		}
	})
	t.Run("paths defaults to the working directory", func(t *testing.T) {
		if diff := cmp.Diff([]string{"."}, cfg.Input.Paths); diff != "" {
			t.Errorf("LoadConfig() input.paths mismatch (-want +got):\n%s", diff)
		}
	})
	t.Run("views defaults to none", func(t *testing.T) {
		if len(cfg.Input.Views) != 0 {
			t.Errorf("LoadConfig() input.views = %q, want []", cfg.Input.Views)
		}
	})
}

// TestLoadConfig_IntrospectListsDefaultToEmpty pins the §4.4 defaults: an
// introspect block that sets one list leaves each other list empty, which the
// PRD reads as "all" for schemas and tables and "none" for exclude_schemas.
func TestLoadConfig_IntrospectListsDefaultToEmpty(t *testing.T) {
	const head = "input:\n  dialect: postgres\n  source: database\n  connection:\n    url: postgres://app@db/appdb\n  introspect:\n"
	tests := []struct {
		name  string
		set   string
		unset func(*config.IntrospectConfig) []string
	}{
		{
			name:  "schemas unset",
			set:   "    tables: [users]\n",
			unset: func(in *config.IntrospectConfig) []string { return in.Schemas },
		},
		{
			name:  "exclude_schemas unset",
			set:   "    schemas: [app]\n",
			unset: func(in *config.IntrospectConfig) []string { return in.ExcludeSchemas },
		},
		{
			name:  "tables unset",
			set:   "    schemas: [app]\n",
			unset: func(in *config.IntrospectConfig) []string { return in.Tables },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tdfLoad(t, head+tt.set+"output:\n  dir: ./models\n")
			if cfg.Input.Introspect == nil {
				t.Fatal("LoadConfig() input.introspect = nil, want the written block")
			}
			if got := tt.unset(cfg.Input.Introspect); len(got) != 0 {
				t.Errorf("LoadConfig() %s = %q, want []", tt.name, got)
			}
		})
	}
}

// TestValidatePreParse_EmptyExpansionStillMeetsRequiredRules pins §4.1.1: a
// variable set to the empty string substitutes "" and the required-field rule
// judges that result, so an empty connection database is reported as missing.
func TestValidatePreParse_EmptyExpansionStillMeetsRequiredRules(t *testing.T) {
	const content = "input:\n  dialect: postgres\n  source: database\n  connection:\n    host: db\n    user: app\n    database: ${TDF_DB_NAME}\noutput:\n  dir: ./models\n"
	t.Setenv("SQLGEN_DB_URL", "")
	t.Setenv("SQLGEN_DB_PASSWORD", "")

	tests := []struct {
		name    string
		value   string
		wantErr string
	}{
		{
			name:    "variable set to the empty string",
			value:   "",
			wantErr: "input.connection.database is required on postgres when input.connection.url and SQLGEN_DB_URL are empty",
		},
		{name: "variable set to a name", value: "appdb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TDF_DB_NAME", tt.value)
			cfg := tdfLoad(t, content)
			if cfg.Input.Connection == nil || cfg.Input.Connection.Database != tt.value {
				t.Fatalf("LoadConfig() input.connection = %+v, want database %q", cfg.Input.Connection, tt.value)
			}
			_, err := config.ValidatePreParse(cfg)
			got := ""
			if err != nil {
				got = err.Error()
			}
			if got != tt.wantErr {
				t.Errorf("ValidatePreParse() error = %q, want %q", got, tt.wantErr)
			}
		})
	}
}

// TestLoadConfig_ExplicitOutputFieldsAreKept pins §4.5: an explicit
// output.package, output.enums.file/package, output.types.file/package and
// output.client.name survive defaulting. output.dir's base is "models", so a
// package re-derived from it would read "models", not "store". The enums and
// types packages may only equal output.package, so their subtests show the
// value is accepted, not that it differs from the default; the rejection of
// any other value is TestValidatePreParse_EnumAndTypeOutputStayInTheModelsPackage.
func TestLoadConfig_ExplicitOutputFieldsAreKept(t *testing.T) {
	cfg := tdfLoad(t, `input:
  dialect: postgres
  paths: ["./migrations"]
output:
  dir: ./models
  package: store
  enums:
    file: pg_enums_gen.go
    package: store
  types:
    file: pg_types_gen.go
    package: store
  client:
    name: Store
`)
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "output.package", got: cfg.Output.Package, want: "store"},
		{name: "output.enums.file", got: cfg.Output.Enums.File, want: "pg_enums_gen.go"},
		{name: "output.enums.package", got: cfg.Output.Enums.Package, want: "store"},
		{name: "output.types.file", got: cfg.Output.Types.File, want: "pg_types_gen.go"},
		{name: "output.types.package", got: cfg.Output.Types.Package, want: "store"},
		{name: "output.client.name", got: cfg.Output.Client.Name, want: "Store"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("LoadConfig() %s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
	t.Run("explicit values pass pre-parse validation", func(t *testing.T) {
		if _, err := config.ValidatePreParse(cfg); err != nil {
			t.Errorf("ValidatePreParse() error = %v, want nil", err)
		}
	})
}

// TestLoadConfig_AbsoluteEnumAndTypeFilesAreKeptAsWritten pins §4.5: an
// absolute output.enums.file or output.types.file is kept verbatim rather
// than joined under output.dir, and one inside output.dir is valid.
func TestLoadConfig_AbsoluteEnumAndTypeFilesAreKeptAsWritten(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	enumsFile := filepath.Join(root, "models", "abs_enums_gen.go")
	typesFile := filepath.Join(root, "models", "abs_types_gen.go")
	cfg := tdfLoad(t, fmt.Sprintf("input:\n  dialect: postgres\n  paths: [\"./migrations\"]\noutput:\n  dir: ./models\n  enums:\n    file: %q\n  types:\n    file: %q\n", enumsFile, typesFile))

	t.Run("enums file", func(t *testing.T) {
		if cfg.Output.Enums.File != enumsFile {
			t.Errorf("LoadConfig() output.enums.file = %q, want %q", cfg.Output.Enums.File, enumsFile)
		}
	})
	t.Run("types file", func(t *testing.T) {
		if cfg.Output.Types.File != typesFile {
			t.Errorf("LoadConfig() output.types.file = %q, want %q", cfg.Output.Types.File, typesFile)
		}
	})
	t.Run("absolute files inside output.dir pass pre-parse validation", func(t *testing.T) {
		if _, err := config.ValidatePreParse(cfg); err != nil {
			t.Errorf("ValidatePreParse() error = %v, want nil", err)
		}
	})
}

// TestPRDFullExample_LoadsAndValidates pins that the §4.12 full example, as
// the PRD prints it, loads and passes pre-parse validation.
func TestPRDFullExample_LoadsAndValidates(t *testing.T) {
	example, err := prdFullExample(tdfReadPRD(t))
	if err != nil {
		t.Fatalf("prdFullExample = %v, want nil", err)
	}
	cfg, err := config.LoadConfig(writeTestConfig(t, example))

	t.Run("loads", func(t *testing.T) {
		if err != nil {
			t.Fatalf("LoadConfig(full example) error = %v, want nil", err)
		}
	})
	t.Run("passes pre-parse validation", func(t *testing.T) {
		if cfg == nil {
			t.Fatal("LoadConfig(full example) failed; nothing to validate")
		}
		if _, err := config.ValidatePreParse(cfg); err != nil {
			t.Errorf("ValidatePreParse(full example) error = %v, want nil", err)
		}
	})
}

// TestPRDAccessExample_LoadsAndValidates pins that the §32.6 column access
// example loads, passes pre-parse validation and matches the config schema.
//
// The block is a tables fragment with no input or output. For loading and
// validation it is appended to the PRD's own §4 "Minimal Config" block, the
// header the PRD says every config can start from. The schema check takes
// the fragment alone, since every root key is optional there.
func TestPRDAccessExample_LoadsAndValidates(t *testing.T) {
	prd := tdfReadPRD(t)
	example := tdfPRDYAMLBlock(t, prd, "### 32.6 Configuration example")
	header := tdfPRDYAMLBlock(t, prd, "### Minimal Config")
	cfg, err := config.LoadConfig(writeTestConfig(t, header+example))

	t.Run("loads", func(t *testing.T) {
		if err != nil {
			t.Fatalf("LoadConfig(minimal config + access example) error = %v, want nil", err)
		}
		col, ok := cfg.Tables["users"].ColumnMap["password_hash"]
		if !ok || col.Access != "internal" {
			t.Errorf("LoadConfig() tables.users.column_map.password_hash = %+v (found %t), want access internal", col, ok)
		}
	})
	t.Run("passes pre-parse validation", func(t *testing.T) {
		if cfg == nil {
			t.Fatal("LoadConfig(minimal config + access example) failed; nothing to validate")
		}
		if _, err := config.ValidatePreParse(cfg); err != nil {
			t.Errorf("ValidatePreParse(access example) error = %v, want nil", err)
		}
	})
	t.Run("matches the config schema", func(t *testing.T) {
		if err := compileSchema(t).Validate(yamlToJSONValue(t, []byte(example))); err != nil {
			t.Errorf("Validate(access example) = %v, want nil", err)
		}
	})
}
