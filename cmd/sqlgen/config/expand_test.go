package config_test

import (
	"os"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// unsetenv unsets name for the test and restores it afterwards.
func unsetenv(t *testing.T, name string) {
	t.Helper()
	t.Setenv(name, "")
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("unsetting %s: %v", name, err)
	}
}

// envReferenceVars are the variables the reference tests set.
var envReferenceVars = map[string]string{
	"EXP_HOST":   "db.internal",
	"EXP_PW":     "hunter2",
	"EXP_LONG":   "correct-horse-battery",
	"EXP_PORT":   "6543",
	"EXP_TRUE":   "true",
	"EXP_NULL":   "null",
	"EXP_TILDE":  "~",
	"EXP_EMPTY":  "",
	"EXP_INJECT": "p@ss: #x\n- y",
	"EXP_NESTED": "${EXP_HOST}",
	"EXP_DIR":    "./schema",
	"EXP_NAME":   "Account",
	"EXP_PRESET": "read_only",
}

func setEnvReferenceVars(t *testing.T) {
	t.Helper()
	for k, v := range envReferenceVars {
		t.Setenv(k, v)
	}
	for _, k := range []string{"EXP_UNSET", "EXP_UNSET_A", "EXP_UNSET_B", "EXP_UNSET_C"} {
		unsetenv(t, k)
	}
}

func TestLoadConfig_EnvReferences(t *testing.T) {
	tests := []struct {
		name  string
		yaml  string
		check func(t *testing.T, cfg *config.RootConfig)
	}{
		{
			name: "dollars outside a reference are kept",
			yaml: "input:\n  connection:\n    host: $1$HOME\n    password: a$$x$\n",
			check: func(t *testing.T, cfg *config.RootConfig) {
				if got := cfg.Input.Connection.Host; got != "$1$HOME" {
					t.Errorf("Host = %q, want %q", got, "$1$HOME")
				}
				if got := cfg.Input.Connection.Password; got != "a$$x$" {
					t.Errorf("Password = %q, want %q", got, "a$$x$")
				}
			},
		},
		{
			name: "whole value",
			yaml: "input:\n  connection:\n    host: ${EXP_HOST}\n",
			check: func(t *testing.T, cfg *config.RootConfig) {
				if got := cfg.Input.Connection.Host; got != "db.internal" {
					t.Errorf("Host = %q, want %q", got, "db.internal")
				}
			},
		},
		{
			name: "several references in one value",
			yaml: "input:\n  connection:\n    url: postgres://app:${EXP_PW}@${EXP_HOST}/d\n",
			check: func(t *testing.T, cfg *config.RootConfig) {
				want := "postgres://app:hunter2@db.internal/d" //nolint:gosec // test fixture, not a real credential
				if got := cfg.Input.Connection.URL; got != want {
					t.Errorf("URL = %q, want %q", got, want)
				}
			},
		},
		{
			name: "a value cannot add structure",
			yaml: "input:\n  connection:\n    password: ${EXP_INJECT}\n",
			check: func(t *testing.T, cfg *config.RootConfig) {
				if got := cfg.Input.Connection.Password; got != "p@ss: #x\n- y" {
					t.Errorf("Password = %q, want the variable's value as one string", got)
				}
			},
		},
		{
			name: "plain scalar typed as int",
			yaml: "input:\n  connection:\n    port: ${EXP_PORT}\n",
			check: func(t *testing.T, cfg *config.RootConfig) {
				if got := cfg.Input.Connection.Port; got != 6543 {
					t.Errorf("Port = %d, want 6543", got)
				}
			},
		},
		{
			name: "plain scalar typed as bool",
			yaml: "api:\n  enabled: ${EXP_TRUE}\n",
			check: func(t *testing.T, cfg *config.RootConfig) {
				if cfg.API == nil || !cfg.API.Enabled {
					t.Errorf("API.Enabled = false, want true")
				}
			},
		},
		{
			name: "null text stays a string",
			yaml: "input:\n  connection:\n    password: ${EXP_NULL}\n    user: ${EXP_TILDE}\n",
			check: func(t *testing.T, cfg *config.RootConfig) {
				if got := cfg.Input.Connection.Password; got != "null" {
					t.Errorf("Password = %q, want %q", got, "null")
				}
				if got := cfg.Input.Connection.User; got != "~" {
					t.Errorf("User = %q, want %q", got, "~")
				}
			},
		},
		{
			name: "empty value into a pointer is an explicit empty string",
			yaml: "tables:\n  users:\n    cache:\n      ttl: ${EXP_EMPTY}\n",
			check: func(t *testing.T, cfg *config.RootConfig) {
				c := cfg.Tables["users"].Cache
				if c == nil || c.TTL == nil || *c.TTL != "" {
					t.Errorf("tables.users.cache.ttl = %v, want a pointer to \"\"", c)
				}
			},
		},
		{
			name: "single pass",
			yaml: "input:\n  connection:\n    host: ${EXP_NESTED}\n",
			check: func(t *testing.T, cfg *config.RootConfig) {
				if got := cfg.Input.Connection.Host; got != "${EXP_HOST}" {
					t.Errorf("Host = %q, want the literal %q", got, "${EXP_HOST}")
				}
			},
		},
		{
			name: "escape",
			yaml: "input:\n  connection:\n    host: a$${EXP_HOST}b$EXP_HOST$1$\n",
			check: func(t *testing.T, cfg *config.RootConfig) {
				if got, want := cfg.Input.Connection.Host, "a${EXP_HOST}b$EXP_HOST$1$"; got != want {
					t.Errorf("Host = %q, want %q", got, want)
				}
			},
		},
		{
			name: "block sequence item",
			yaml: "input:\n  paths:\n    - ${EXP_DIR}\n    - b\n",
			check: func(t *testing.T, cfg *config.RootConfig) {
				if got := strings.Join(cfg.Input.Paths, ","); got != "./schema,b" {
					t.Errorf("Paths = %q, want %q", got, "./schema,b")
				}
			},
		},
		{
			name: "quoted flow sequence item",
			yaml: "input:\n  paths: [\"${EXP_DIR}\", b]\n",
			check: func(t *testing.T, cfg *config.RootConfig) {
				if got := strings.Join(cfg.Input.Paths, ","); got != "./schema,b" {
					t.Errorf("Paths = %q, want %q", got, "./schema,b")
				}
			},
		},
		{
			name: "keys are not expanded",
			yaml: "tables:\n  ${EXP_HOST}:\n    struct_name: ${EXP_NAME}\n",
			check: func(t *testing.T, cfg *config.RootConfig) {
				tc, ok := cfg.Tables["${EXP_HOST}"]
				if !ok {
					t.Fatalf("Tables has no literal ${EXP_HOST} key: %v", cfg.Tables)
				}
				if tc.StructName != "Account" {
					t.Errorf("StructName = %q, want %q", tc.StructName, "Account")
				}
			},
		},
		{
			name: "string tag",
			yaml: "input:\n  connection:\n    password: !!str ${EXP_PORT}\n",
			check: func(t *testing.T, cfg *config.RootConfig) {
				if got := cfg.Input.Connection.Password; got != "6543" {
					t.Errorf("Password = %q, want %q", got, "6543")
				}
			},
		},
		{
			name: "operations preset",
			yaml: "api:\n  operations: ${EXP_PRESET}\n",
			check: func(t *testing.T, cfg *config.RootConfig) {
				if cfg.API.Operations == nil || cfg.API.Operations.Preset != "read_only" {
					t.Errorf("api.operations = %+v, want preset read_only", cfg.API.Operations)
				}
			},
		},
		{
			name: "operations mapping",
			yaml: "api:\n  operations:\n    create: ${EXP_TRUE}\n",
			check: func(t *testing.T, cfg *config.RootConfig) {
				ops := cfg.API.Operations
				if ops == nil || ops.Create == nil || !*ops.Create {
					t.Errorf("api.operations.create = %+v, want true", ops)
				}
			},
		},
		{
			name: "anchor and alias",
			yaml: "input:\n  connection:\n    host: &h ${EXP_HOST}\n    database: *h\n",
			check: func(t *testing.T, cfg *config.RootConfig) {
				c := cfg.Input.Connection
				if c.Host != "db.internal" || c.Database != "db.internal" {
					t.Errorf("Host, Database = %q, %q, want both %q", c.Host, c.Database, "db.internal")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnvReferenceVars(t)
			cfg, err := config.LoadConfig(writeTestConfig(t, tt.yaml+"output:\n  dir: ./models\n"))
			if err != nil {
				t.Fatalf("LoadConfig() unexpected error: %v", err)
			}
			tt.check(t, cfg)
		})
	}
}

func TestLoadConfig_EnvReferenceErrors(t *testing.T) {
	const (
		invalidName = `a name is letters, digits and '_', not starting with a digit`
		portType    = `cannot unmarshal the expansion of "${%s}" into int`
	)
	tests := []struct {
		name string
		yaml string
		// want lists messages that must appear in this order.
		want []string
		// notWant lists messages that must not appear.
		notWant []string
	}{
		{
			name: "one unset variable",
			yaml: "input:\n  connection:\n    password: ${EXP_UNSET}\n",
			want: []string{"line 3: input.connection.password: environment variable EXP_UNSET is not set"},
		},
		{
			name: "every unset variable is reported",
			yaml: "input:\n  connection:\n    url: postgres://${EXP_UNSET_A}:${EXP_UNSET_B}@h/d\n    host: ${EXP_UNSET_C}\n",
			want: []string{
				"line 3: input.connection.url: environment variable EXP_UNSET_A is not set",
				"line 3: input.connection.url: environment variable EXP_UNSET_B is not set",
				"line 4: input.connection.host: environment variable EXP_UNSET_C is not set",
			},
		},
		{
			name: "sequence item path",
			yaml: "input:\n  paths:\n    - a\n    - ${EXP_UNSET}\n",
			want: []string{"line 4: input.paths[1]: environment variable EXP_UNSET is not set"},
		},
		{
			name: "a default is an invalid reference",
			yaml: "input:\n  connection:\n    host: ${EXP_HOST:-x}\n",
			want: []string{`line 3: input.connection.host: invalid reference "${EXP_HOST:-x}": ` + invalidName},
		},
		{
			name: "empty and digit-leading names",
			yaml: "input:\n  connection:\n    host: ${}\n    user: ${1X}\n",
			want: []string{
				`line 3: input.connection.host: invalid reference "${}": ` + invalidName,
				`line 4: input.connection.user: invalid reference "${1X}": ` + invalidName,
			},
		},
		{
			name: "unterminated reference",
			yaml: "input:\n  connection:\n    host: ${EXP_HOST\n",
			want: []string{`line 3: input.connection.host: unterminated reference "${EXP_HOST"`},
		},
		{
			name:    "reference errors come before decoding",
			yaml:    "input:\n  typo: x\n  connection:\n    port: ${EXP_UNSET}\n",
			want:    []string{"line 4: input.connection.port: environment variable EXP_UNSET is not set"},
			notWant: []string{"typo"},
		},
		{
			name:    "explicitly tagged reference",
			yaml:    "input:\n  typo: x\n  connection:\n    port: !!int ${EXP_LONG}\n    host: !!str ${EXP_HOST}\n",
			want:    []string{"line 4: input.connection.port: a reference can't be in a !!int-tagged value; leave it untagged, it is typed by its value"},
			notWant: []string{"typo", "input.connection.host"},
		},
		{
			name: "short value in a type error is redacted",
			yaml: "input:\n  connection:\n    port: ${EXP_PW}\n",
			want: []string{"line 3: input.connection.port: " + strings.ReplaceAll(portType, "%s", "EXP_PW")},
		},
		{
			name: "long value in a type error is redacted",
			yaml: "input:\n  connection:\n    port: ${EXP_LONG}\n",
			want: []string{"line 3: input.connection.port: " + strings.ReplaceAll(portType, "%s", "EXP_LONG")},
		},
		{
			name: "quoted reference is a string",
			yaml: "input:\n  connection:\n    port: \"${EXP_PORT}\"\n",
			want: []string{"line 3: input.connection.port: " + strings.ReplaceAll(portType, "%s", "EXP_PORT")},
		},
		{
			name: "empty value into an int",
			yaml: "input:\n  connection:\n    port: ${EXP_EMPTY}\n",
			want: []string{"line 3: input.connection.port: " + strings.ReplaceAll(portType, "%s", "EXP_EMPTY")},
		},
		{
			name: "unquoted reference in a flow sequence",
			yaml: "input:\n  paths: [${EXP_DIR}, b]\n",
			want: []string{"parsing config: yaml: line 1: did not find expected ',' or ']'"},
		},
		{
			name:    "unknown key at its real line",
			yaml:    "input:\n  connection:\n    host: ${EXP_HOST}\n\n# a comment\n\n  # another\n  typo: x\n",
			want:    []string{"line 8: field typo not found in type config.InputConfig"},
			notWant: []string{"EXP_HOST"},
		},
		{
			name: "unknown key and reference type error in line order",
			yaml: "input:\n  typo: x\n  connection:\n    port: ${EXP_PW}\n",
			want: []string{
				"line 2: field typo not found in type config.InputConfig",
				"line 4: input.connection.port: " + strings.ReplaceAll(portType, "%s", "EXP_PW"),
			},
		},
		{
			name: "unknown key beside a typed reference in an operations mapping",
			yaml: "api:\n  operations:\n    create: ${EXP_TRUE}\n  typo: x\n",
			want: []string{"line 4: field typo not found in type config.APIConfig"},
		},
		{
			name: "duplicate key with a reference",
			yaml: "input:\n  connection:\n    host: ${EXP_HOST}\n    host: other\n",
			want: []string{`line 4: mapping key "host" already defined at line 3`},
		},
		{
			name: "type error away from a reference keeps its message",
			yaml: "input:\n  connection:\n    host: ${EXP_HOST}\n    port: not-a-number\n",
			want: []string{"line 4: cannot unmarshal !!str `not-a-n...` into int"},
		},
		{
			name: "sequence beside a reference keeps its message",
			yaml: "input:\n  connection:\n    port: [\"${EXP_PORT}\"]\n",
			want: []string{"line 3: cannot unmarshal !!seq into int"},
		},
		{
			name: "type error in an operations mapping is reported with the rest",
			yaml: "api:\n  operations:\n    create: notabool\n  typo: x\n",
			want: []string{
				"line 3: cannot unmarshal !!str `notabool` into bool",
				"line 4: field typo not found in type config.APIConfig",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnvReferenceVars(t)
			_, err := config.LoadConfig(writeTestConfig(t, tt.yaml+"output:\n  dir: ./models\n"))
			if err == nil {
				t.Fatal("LoadConfig() error = nil, want an error")
			}
			msg := err.Error()
			at := 0
			for _, w := range tt.want {
				i := strings.Index(msg[at:], w)
				if i < 0 {
					t.Fatalf("LoadConfig() error = %q, want it to contain %q after offset %d", msg, w, at)
				}
				at += i + len(w)
			}
			for _, w := range tt.notWant {
				if strings.Contains(msg, w) {
					t.Errorf("LoadConfig() error = %q, want it not to contain %q", msg, w)
				}
			}
			// yaml.v3 truncates a long value to its first 7 characters, so
			// the check looks for prefixes.
			for _, secret := range []string{"hunter", "correct", "db.inte", "6543", "p@ss"} {
				if strings.Contains(msg, secret) {
					t.Errorf("LoadConfig() error = %q contains an expanded value %q", msg, secret)
				}
			}
		})
	}
}
