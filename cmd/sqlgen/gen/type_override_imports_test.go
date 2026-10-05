package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// typeOverrideImportConfig returns a fully-defaulted config over
// typeOverrideSchema, ready for a by-SQL-type override to be dropped into.
// ValidateGeneration is the entry point under test rather than
// BuildTableContexts, because the rule is registered on the seam all three
// context-building entry points share — and `sqlgen validate` is where a
// consumer should hear about it.
func typeOverrideImportConfig(t *testing.T) (*parser.Schema, *config.RootConfig) {
	t.Helper()
	schema := typeOverrideSchema()
	return schema, testInput(schema).Config
}

// TestTypeOverrideRejectsUnimportableLiteral pins the by-SQL-type
// declaration forms to the same import rule the two per-column forms enforce.
//
// Without it, goimports back-fills the omission and the package binds something
// the config never named — the standard library `uuid` Go 1.27 ships, whatever
// the generating machine's module cache holds, or nothing at all. The
// `uuid.UUID` cases are the ones that also defeat the §4.13 one-library rule:
// an import-less declaration is not a claim collectUUIDClaims can see.
func TestTypeOverrideRejectsUnimportableLiteral(t *testing.T) {
	tests := []struct {
		name    string
		apply   func(*config.RootConfig)
		wantErr string
	}{
		{
			name: "global overrides.types without an import",
			apply: func(cfg *config.RootConfig) {
				cfg.Overrides.Types["numeric"] = config.TypeOverride{Type: "decimal.Decimal"}
			},
			wantErr: "overrides.types.numeric.type",
		},
		{
			name: "global overrides.types naming a UUID library without an import",
			apply: func(cfg *config.RootConfig) {
				cfg.Overrides.Types["uuid"] = config.TypeOverride{Type: "uuid.UUID"}
			},
			wantErr: "overrides.types.uuid.type",
		},
		{
			name: "table-scoped overrides.types without an import",
			apply: func(cfg *config.RootConfig) {
				cfg.Tables["sessions"] = config.TableConfig{
					Overrides: &config.OverrideConfig{
						Types: map[string]config.TypeOverride{
							"uuid": {Type: "uuid.UUID"},
						},
					},
				}
			},
			wantErr: "tables.sessions.overrides.types.uuid.type",
		},
		{
			name: "a nullable variant with no import to inherit",
			apply: func(cfg *config.RootConfig) {
				cfg.Overrides.Types["text"] = config.TypeOverride{
					Type:     "Address",
					Nullable: config.NullableVariant{Type: "nullable.Address"},
				}
			},
			wantErr: "overrides.types.text.nullable.type",
		},
		{
			name: "an extras field without an import",
			apply: func(cfg *config.RootConfig) {
				cfg.Extras["payload"] = config.ExtraType{
					Fields: map[string]config.ExtraTypeField{
						"trace_id": {Type: "uuid.UUID"},
					},
				}
			},
			wantErr: "extras.payload.fields.trace_id.type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, cfg := typeOverrideImportConfig(t)
			tt.apply(cfg)

			_, err := gen.ValidateGeneration(schema, cfg)
			if err == nil {
				t.Fatalf("ValidateGeneration() error = nil, want one mentioning %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ValidateGeneration() error = %q, want it to mention %q", err, tt.wantErr)
			}
			// The message has to name the field that fixes it, not just the
			// one that is wrong — the same shape the column_map form produces.
			if !strings.Contains(err.Error(), ".import") {
				t.Errorf("ValidateGeneration() error = %q, want it to name the .import field to set", err)
			}
		})
	}
}

// TestTypeOverrideAcceptsImportableLiteral is the negative control. The
// builtin cases are not hypothetical: the `graphql` example's
// `tables.scalar_probes.overrides.types` block declares six of them, so a rule
// that required an import unconditionally — the shape
// config.validateTenancyTypeOverride uses — would reject a shipped example.
func TestTypeOverrideAcceptsImportableLiteral(t *testing.T) {
	tests := []struct {
		name  string
		apply func(*config.RootConfig)
	}{
		{
			name: "builtin needs no import",
			apply: func(cfg *config.RootConfig) {
				cfg.Overrides.Types["numeric"] = config.TypeOverride{Type: "float64"}
			},
		},
		{
			name: "registry member supplies its own import",
			apply: func(cfg *config.RootConfig) {
				cfg.Overrides.Types["numeric"] = config.TypeOverride{Type: "time.Time"}
			},
		},
		{
			name: "same-package type needs none",
			apply: func(cfg *config.RootConfig) {
				cfg.Overrides.Types["numeric"] = config.TypeOverride{Type: "Address"}
			},
		},
		{
			name: "external type with its import declared",
			apply: func(cfg *config.RootConfig) {
				cfg.Overrides.Types["numeric"] = config.TypeOverride{
					Type:   "decimal.Decimal",
					Import: "github.com/shopspring/decimal",
				}
			},
		},
		{
			name: "the nullable shorthand inherits the parent import",
			apply: func(cfg *config.RootConfig) {
				cfg.Overrides.Types["uuid"] = config.TypeOverride{
					Type:     "uuid.UUID",
					Import:   "github.com/google/uuid",
					Nullable: config.NullableVariant{Type: "uuid.NullUUID"},
				}
			},
		},
		{
			name: "table-scoped builtins need no import",
			apply: func(cfg *config.RootConfig) {
				cfg.Tables["sessions"] = config.TableConfig{
					Overrides: &config.OverrideConfig{
						Types: map[string]config.TypeOverride{
							"numeric": {Type: "float64"},
							"text":    {Type: "string"},
						},
					},
				}
			},
		},
		{
			name: "an extras field on a builtin",
			apply: func(cfg *config.RootConfig) {
				cfg.Extras["payload"] = config.ExtraType{
					Fields: map[string]config.ExtraTypeField{
						"view_count": {Type: "int"},
						"seen_at":    {Type: "time.Time"},
					},
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, cfg := typeOverrideImportConfig(t)
			tt.apply(cfg)

			if _, err := gen.ValidateGeneration(schema, cfg); err != nil {
				t.Errorf("ValidateGeneration() error = %v, want nil", err)
			}
		})
	}
}

// TestTypeOverrideImportsReportedTogether pins that one run answers for the
// whole config: `sqlgen validate` should not have to be run once per bad entry.
func TestTypeOverrideImportsReportedTogether(t *testing.T) {
	schema, cfg := typeOverrideImportConfig(t)
	cfg.Overrides.Types["numeric"] = config.TypeOverride{Type: "decimal.Decimal"}
	cfg.Extras["payload"] = config.ExtraType{
		Fields: map[string]config.ExtraTypeField{"trace_id": {Type: "uuid.UUID"}},
	}

	_, err := gen.ValidateGeneration(schema, cfg)
	if err == nil {
		t.Fatal("ValidateGeneration() error = nil, want one naming both violations")
	}
	for _, want := range []string{
		"overrides.types.numeric.type",
		"extras.payload.fields.trace_id.type",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ValidateGeneration() error = %q, want it to mention %q", err, want)
		}
	}
}

// TestTypeOverrideImportsUnmatchedTableKey pins the reason the rule cannot ride
// the per-table seam validateColumnTypeLiterals uses: that seam runs inside
// buildSingleTableContext, once per table *in the schema*, so a `tables.<t>`
// block whose key matches no table is never visited. A typo'd table name is
// exactly when a consumer most needs to hear about the rest of the block.
func TestTypeOverrideImportsUnmatchedTableKey(t *testing.T) {
	schema, cfg := typeOverrideImportConfig(t)
	// No such table — typeOverrideSchema declares `sessions` alone.
	cfg.Tables["sesions"] = config.TableConfig{
		Overrides: &config.OverrideConfig{
			Types: map[string]config.TypeOverride{"uuid": {Type: "uuid.UUID"}},
		},
	}

	_, err := gen.ValidateGeneration(schema, cfg)
	if err == nil {
		t.Fatal("ValidateGeneration() error = nil, want the override reported even though no table matches the key")
	}
	if want := "tables.sesions.overrides.types.uuid.type"; !strings.Contains(err.Error(), want) {
		t.Errorf("ValidateGeneration() error = %q, want it to mention %q", err, want)
	}
}

// TestTypeOverrideImportsReachesGraphQLGen pins the third entry point.
// BuildEntityContextsFromSchema is what `sqlgen graphql gen` builds through,
// and it is the path a rule registered at the call sites silently escapes.
// Registering on validateResolvedPackage is what makes it reachable; this
// asserts that rather than trusting the comment.
func TestTypeOverrideImportsReachesGraphQLGen(t *testing.T) {
	schema, cfg := typeOverrideImportConfig(t)
	cfg.Overrides.Types["uuid"] = config.TypeOverride{Type: "uuid.UUID"}

	_, _, err := gen.BuildEntityContextsFromSchema(schema, cfg)
	if err == nil {
		t.Fatal("BuildEntityContextsFromSchema() error = nil, want the graphql gen path to reject it too")
	}
	if want := "overrides.types.uuid.type"; !strings.Contains(err.Error(), want) {
		t.Errorf("BuildEntityContextsFromSchema() error = %q, want it to mention %q", err, want)
	}
}

// TestTypeOverrideImportsRejectsUninheritableNullWrapper is the shape the
// reviewer of this fix flagged as newly rejected: a builtin parent with a
// shorthand `nullable: sql.NullString`. It is rejected deliberately, because it
// never worked. Measured before the rule existed: the parent carries no import,
// so `database/sql` never enters the package's import set;
// aliasStdSQLQualifiers (format.go) then declines to rewrite `sql.NullString`
// to `stdsql.NullString` precisely because the file does not import
// database/sql; and the emitted struct field spells a bare `sql.NullString`
// that binds either to nothing or — in any package that imports it — to
// sqlgen's own runtime `sql` package, which has no NullString.
func TestTypeOverrideImportsRejectsUninheritableNullWrapper(t *testing.T) {
	schema, cfg := typeOverrideImportConfig(t)
	cfg.Overrides.Types["text"] = config.TypeOverride{
		Type:     "string",
		Nullable: config.NullableVariant{Type: "sql.NullString"},
	}

	_, err := gen.ValidateGeneration(schema, cfg)
	if err == nil {
		t.Fatal("ValidateGeneration() error = nil, want the uninheritable null wrapper rejected")
	}
	if want := "overrides.types.text.nullable.type"; !strings.Contains(err.Error(), want) {
		t.Errorf("ValidateGeneration() error = %q, want it to mention %q", err, want)
	}
}
