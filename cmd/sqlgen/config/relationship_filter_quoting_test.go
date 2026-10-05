package config_test

import (
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// mysqlConfig is validConfig retargeted at MySQL. `pgx` is postgres-only
// (validateDriverDialect), so the driver moves with the dialect.
func mysqlConfig() *config.RootConfig {
	cfg := validConfig()
	cfg.Input.Dialect = config.DialectMySQL
	cfg.Output.Driver = config.DriverStdlib
	return cfg
}

func filterRel(filter string) config.TableRelationship {
	return config.TableRelationship{
		Name: "Attachments", Type: "o2m", Table: "documents", FK: "entity_id",
		Filter: filter,
	}
}

// TestValidateRelationships_MySQLDoubleQuotedFilterRejected pins the guard on
// the one dialect where `"x"` is a string literal rather than a quoted
// identifier.
//
// Left ungated, `filter: "\"entity_type\" = 'asset.primary'"` reaches MySQL as
// `'entity_type' = 'asset.primary'` — two constants, false for every row, and
// silent: a string-vs-string comparison raises no warning at all, and the
// string-vs-number form raises only warning 1292. The relationship loads
// nothing and nothing says why.
//
// Nor is it recoverable downstream. ANSI_QUOTES fixes only the o2m/m2m loader,
// which hands the filter text to MySQL verbatim; the o2o JOIN ON and the
// relationship-filter EXISTS are qualified by vitess at *generation* time, so
// the literal is baked into the generated source before MySQL ever sees it.
// One `filter:` would then mean two different things on two read paths of the
// same edge, which is why this is a hard error rather than a documented
// footnote (PRD §13.7.1).
func TestValidateRelationships_MySQLDoubleQuotedFilterRejected(t *testing.T) {
	tests := []struct {
		name   string
		filter string
	}{
		{"quoted identifier", `"entity_type" = 'asset.primary'`},
		{"quoted identifier among bare ones", `"name" LIKE 'photo_%' AND status = 'live'`},
		{"double-quoted value", `entity_type = "asset.primary"`},
		{"reserved-word column", `"order" = 1`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := mysqlConfig()
			cfg.Tables["assets"] = config.TableConfig{
				Relationships: []config.TableRelationship{filterRel(tt.filter)},
			}

			_, err := config.ValidatePreParse(cfg)
			assertErrContains(t, err, "ValidatePreParse()",
				"tables.assets.relationships[0]", "Attachments", "double-quoted", "string literal")
		})
	}
}

// TestValidateRelationships_MySQLFilterQuotingAccepted is the other half: the
// guard must not reject ordinary MySQL. A double quote inside a string literal
// or a backtick-quoted identifier is not a quoted-identifier mistake, so the
// scan tracks both rather than searching for the bare character.
func TestValidateRelationships_MySQLFilterQuotingAccepted(t *testing.T) {
	tests := []struct {
		name   string
		filter string
	}{
		{"bare identifiers", `entity_type = 'asset.primary'`},
		{"backtick identifier", "`entity_type` = 'asset.primary'"},
		{"backtick reserved word", "`order` = 1"},
		{"double quote inside a string literal", `note = 'say "hi"'`},
		{"double quote inside a backtick identifier", "`we\"ird` = 1"},
		{"doubled single quote before a double quote", `note = 'it''s "x"'`},
		{"backslash-escaped quote inside a literal", `note = 'a\'b'`},
		{"no filter", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := mysqlConfig()
			cfg.Tables["assets"] = config.TableConfig{
				Relationships: []config.TableRelationship{filterRel(tt.filter)},
			}

			if _, err := config.ValidatePreParse(cfg); err != nil {
				t.Errorf("ValidatePreParse() = %v, want nil", err)
			}
		})
	}
}

// TestValidateRelationships_DoubleQuotedFilterIsMySQLOnly keeps the guard off
// the dialects where `"x"` is the standard quoted identifier and means exactly
// what the author wrote. Rejecting it there would outlaw the only spelling
// PostgreSQL accepts for a reserved-word column.
func TestValidateRelationships_DoubleQuotedFilterIsMySQLOnly(t *testing.T) {
	for _, dialect := range []config.Dialect{config.DialectPostgres, config.DialectSQLite} {
		t.Run(string(dialect), func(t *testing.T) {
			cfg := validConfig()
			cfg.Input.Dialect = dialect
			if dialect != config.DialectPostgres {
				cfg.Output.Driver = config.DriverStdlib
			}
			cfg.Tables["assets"] = config.TableConfig{
				Relationships: []config.TableRelationship{filterRel(`"order" = 1`)},
			}

			if _, err := config.ValidatePreParse(cfg); err != nil {
				t.Errorf("ValidatePreParse() = %v, want nil", err)
			}
		})
	}
}
