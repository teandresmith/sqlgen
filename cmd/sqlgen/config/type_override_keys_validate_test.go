package config_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// TestValidatePreParse_PostgresSynonymTypeKey pins that, under PostgreSQL, an
// overrides.types key spelled as a synonym the parser never writes (int4,
// character varying(10), timestamp with time zone) is a config error naming
// the spelling to key it by instead. Such a key matches no column, so the
// override would otherwise be dropped without a word.
func TestValidatePreParse_PostgresSynonymTypeKey(t *testing.T) {
	tests := []struct {
		name    string
		dialect config.Dialect
		key     string
		want    string
	}{
		{name: "int4", dialect: config.DialectPostgres, key: "int4", want: "integer"},
		{name: "int", dialect: config.DialectPostgres, key: "int", want: "integer"},
		{name: "int2", dialect: config.DialectPostgres, key: "int2", want: "smallint"},
		{name: "int8", dialect: config.DialectPostgres, key: "int8", want: "bigint"},
		{name: "serial", dialect: config.DialectPostgres, key: "serial", want: "integer"},
		{name: "bigserial", dialect: config.DialectPostgres, key: "bigserial", want: "bigint"},
		{name: "smallserial", dialect: config.DialectPostgres, key: "smallserial", want: "smallint"},
		{name: "float4", dialect: config.DialectPostgres, key: "float4", want: "real"},
		{name: "float8", dialect: config.DialectPostgres, key: "float8", want: "double precision"},
		{name: "float", dialect: config.DialectPostgres, key: "float", want: "double precision"},
		{name: "float with single precision", dialect: config.DialectPostgres, key: "float(10)", want: "real"},
		{name: "float with double precision", dialect: config.DialectPostgres, key: "float(30)", want: "double precision"},
		{name: "bool", dialect: config.DialectPostgres, key: "bool", want: "boolean"},
		{name: "bpchar", dialect: config.DialectPostgres, key: "bpchar", want: "char"},
		{name: "character", dialect: config.DialectPostgres, key: "character", want: "char"},
		{name: "character with length", dialect: config.DialectPostgres, key: "character(5)", want: "char(5)"},
		{name: "bpchar with length", dialect: config.DialectPostgres, key: "bpchar(5)", want: "char(5)"},
		{name: "character varying", dialect: config.DialectPostgres, key: "character varying", want: "varchar"},
		{name: "character varying with length", dialect: config.DialectPostgres, key: "character varying(10)", want: "varchar(10)"},
		{name: "decimal", dialect: config.DialectPostgres, key: "decimal", want: "numeric"},
		{name: "decimal with precision and scale", dialect: config.DialectPostgres, key: "decimal(10,2)", want: "numeric(10,2)"},
		{name: "timestamp with time zone", dialect: config.DialectPostgres, key: "timestamp with time zone", want: "timestamptz"},
		{name: "timestamp with time zone and precision", dialect: config.DialectPostgres, key: "timestamp(3) with time zone", want: "timestamptz(3)"},
		{name: "timestamp without time zone", dialect: config.DialectPostgres, key: "timestamp without time zone", want: "timestamp"},
		{name: "time with time zone", dialect: config.DialectPostgres, key: "time with time zone", want: "timetz"},
		{name: "time without time zone", dialect: config.DialectPostgres, key: "time without time zone", want: "time"},
		{name: "bit varying", dialect: config.DialectPostgres, key: "bit varying", want: "varbit"},
		{name: "bit varying with length", dialect: config.DialectPostgres, key: "bit varying(4)", want: "varbit(4)"},
		{name: "int4 array", dialect: config.DialectPostgres, key: "int4[]", want: "integer[]"},
		{name: "character varying array with length", dialect: config.DialectPostgres, key: "character varying(10)[]", want: "varchar(10)[]"},
		{name: "integer accepted", dialect: config.DialectPostgres, key: "integer"},
		{name: "double precision accepted", dialect: config.DialectPostgres, key: "double precision"},
		{name: "varchar with length accepted", dialect: config.DialectPostgres, key: "varchar(10)"},
		{name: "timestamptz accepted", dialect: config.DialectPostgres, key: "timestamptz"},
		{name: "integer array accepted", dialect: config.DialectPostgres, key: "integer[]"},
		{name: "uuid accepted", dialect: config.DialectPostgres, key: "uuid"},
		{name: "mysql unaffected", dialect: config.DialectMySQL, key: "int4"},
		{name: "sqlite unaffected", dialect: config.DialectSQLite, key: "int4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			override := config.TypeOverride{Type: "int64"}
			scopes := []struct {
				path string
				set  func(*config.RootConfig)
			}{
				{
					path: "overrides.types." + tt.key,
					set:  func(cfg *config.RootConfig) { cfg.Overrides.Types[tt.key] = override },
				},
				{
					path: "tables.public.users.overrides.types." + tt.key,
					set: func(cfg *config.RootConfig) {
						cfg.Tables["public.users"] = config.TableConfig{
							Overrides: &config.OverrideConfig{Types: map[string]config.TypeOverride{tt.key: override}},
						}
					},
				},
			}
			for _, scope := range scopes {
				cfg := validConfig()
				cfg.Input.Dialect = tt.dialect
				if tt.dialect != config.DialectPostgres {
					cfg.Output.Driver = config.DriverStdlib
				}
				scope.set(cfg)

				_, err := config.ValidatePreParse(cfg)
				got := ""
				if err != nil {
					got = err.Error()
				}
				want := ""
				if tt.want != "" {
					want = scope.path + `: PostgreSQL writes this type as "` + tt.want + `"; key it "` + tt.want + `"`
				}
				if diff := cmp.Diff(want, got); diff != "" {
					t.Errorf("ValidatePreParse() with %s error mismatch (-want +got):\n%s", scope.path, diff)
				}
			}
		})
	}
}

// TestValidatePreParse_TypeKeyLetterCase pins that override keys are read
// case-insensitively: a synonym in another case is still a synonym, and two
// keys of one scope that differ only in case are rejected.
func TestValidatePreParse_TypeKeyLetterCase(t *testing.T) {
	tests := []struct {
		name string
		keys []string
		want string
	}{
		{
			name: "upper-case synonym",
			keys: []string{"INT4"},
			want: `overrides.types.INT4: PostgreSQL writes this type as "integer"; key it "integer"`,
		},
		{
			name: "keys that differ only in case",
			keys: []string{"NUMERIC", "numeric"},
			want: `overrides.types: keys "NUMERIC" and "numeric" name the same type; keep one`,
		},
		{
			name: "one upper-case canonical key",
			keys: []string{"NUMERIC"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			for _, key := range tt.keys {
				cfg.Overrides.Types[key] = config.TypeOverride{Type: "int64"}
			}
			_, err := config.ValidatePreParse(cfg)
			got := ""
			if err != nil {
				got = err.Error()
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ValidatePreParse() error mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
