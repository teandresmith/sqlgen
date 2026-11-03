package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// TestReservedPackageNamesMatrix walks every name PRD §8.5 lists under "No
// entity may claim a name the generated package already declares" and shows
// that each one is reserved against every entity kind that claims a Go type
// name (a table, an enum, a composite type, a domain type), and that each fixed
// file stem is reserved against a table's stem.
//
// The cache and event-hook features stay off throughout: §8.5 says a
// feature-gated artifact reserves its names whether or not the feature is on.
//
// sqlName is the SQL name an enum, composite or domain type is given so that
// its Go type name resolves to the reserved name; enumStructName is set only
// where singularization would move the enum off it.
func TestReservedPackageNamesMatrix(t *testing.T) {
	tests := []struct {
		name           string
		row            string
		sqlName        string
		enumStructName string
		stem           bool
		clientFile     string
		enumsFile      string
		typesFile      string
	}{
		{name: "Client", row: "reserved-pkg-client", sqlName: "client"},
		{name: "ClientOption", row: "reserved-pkg-client", sqlName: "client_option"},
		{name: "New", row: "reserved-pkg-client", sqlName: "new"},
		{name: "WithCallbackMode", row: "reserved-pkg-client", sqlName: "with_callback_mode"},
		{name: "WithPanicHandler", row: "reserved-pkg-client", sqlName: "with_panic_handler"},
		{name: "WithMutationHook", row: "reserved-pkg-client", sqlName: "with_mutation_hook"},
		{name: "WithQueryHook", row: "reserved-pkg-client", sqlName: "with_query_hook"},
		{name: "WithCache", row: "reserved-pkg-client", sqlName: "with_cache"},
		{name: "WithTenantResolver", row: "reserved-pkg-client", sqlName: "with_tenant_resolver"},

		{name: "Connection", row: "reserved-pkg-cursor", sqlName: "connection"},
		{name: "ConnectionInput", row: "reserved-pkg-cursor", sqlName: "connection_input"},
		{name: "Edge", row: "reserved-pkg-cursor", sqlName: "edge"},
		{name: "PageInfo", row: "reserved-pkg-cursor", sqlName: "page_info"},

		{name: "PaginateInput", row: "reserved-pkg-offset", sqlName: "paginate_input"},
		{name: "PaginateResult", row: "reserved-pkg-offset", sqlName: "paginate_result"},

		{name: "CallOptions", row: "reserved-pkg-shared", sqlName: "call_options", enumStructName: "CallOptions"},
		{name: "IncrementInput", row: "reserved-pkg-shared", sqlName: "increment_input"},

		{name: "ErrNotFound", row: "reserved-pkg-errors", sqlName: "err_not_found"},
		{name: "ErrConstraintViolation", row: "reserved-pkg-errors", sqlName: "err_constraint_violation"},
		{name: "ErrDeadlock", row: "reserved-pkg-errors", sqlName: "err_deadlock"},
		{name: "ErrConnectionFailed", row: "reserved-pkg-errors", sqlName: "err_connection_failed"},
		{name: "ErrInvalidCursor", row: "reserved-pkg-errors", sqlName: "err_invalid_cursor"},
		{name: "ErrEmptyFilter", row: "reserved-pkg-errors", sqlName: "err_empty_filter"},
		{name: "ErrAmbiguousFilter", row: "reserved-pkg-errors", sqlName: "err_ambiguous_filter"},
		{name: "ErrNilInput", row: "reserved-pkg-errors", sqlName: "err_nil_input"},
		{name: "ErrAlreadyRelated", row: "reserved-pkg-errors", sqlName: "err_already_related"},
		{name: "ErrNestedVerbConflict", row: "reserved-pkg-errors", sqlName: "err_nested_verb_conflict"},
		{name: "ErrTxDone", row: "reserved-pkg-errors", sqlName: "err_tx_done"},
		{name: "ConstraintError", row: "reserved-pkg-errors", sqlName: "constraint_error"},
		{name: "ConstraintType", row: "reserved-pkg-errors", sqlName: "constraint_type"},
		{name: "ConstraintCheck", row: "reserved-pkg-errors", sqlName: "constraint_check"},
		{name: "ConstraintForeignKey", row: "reserved-pkg-errors", sqlName: "constraint_foreign_key"},
		{name: "ConstraintNotNull", row: "reserved-pkg-errors", sqlName: "constraint_not_null"},
		{name: "ConstraintUnique", row: "reserved-pkg-errors", sqlName: "constraint_unique"},
		{name: "NestedMutationError", row: "reserved-pkg-errors", sqlName: "nested_mutation_error"},

		{name: "Cache", row: "reserved-pkg-cache", sqlName: "cache"},
		{name: "CacheOption", row: "reserved-pkg-cache", sqlName: "cache_option"},
		{name: "NewCache", row: "reserved-pkg-cache", sqlName: "new_cache"},
		{name: "WithSerializer", row: "reserved-pkg-cache", sqlName: "with_serializer"},
		{name: "WithCircuitBreaker", row: "reserved-pkg-cache", sqlName: "with_circuit_breaker"},
		{name: "WithInvalidationSource", row: "reserved-pkg-cache", sqlName: "with_invalidation_source"},
		{name: "WithMetricsRecorder", row: "reserved-pkg-cache", sqlName: "with_metrics_recorder"},
		{name: "WithOnError", row: "reserved-pkg-cache", sqlName: "with_on_error"},

		{name: "WithEventPublisher", row: "reserved-pkg-events", sqlName: "with_event_publisher"},

		// Stem rows: sqlName is the stem itself, reached through a table's
		// struct_name below.
		{name: "models", row: "reserved-pkg-stems", sqlName: "models", stem: true},
		{name: "views", row: "reserved-pkg-stems", sqlName: "views", stem: true},
		{name: "errors", row: "reserved-pkg-stems", sqlName: "errors", stem: true},
		{name: "tablenames", row: "reserved-pkg-stems", sqlName: "tablenames", stem: true},
		{name: "sorter", row: "reserved-pkg-stems", sqlName: "sorter", stem: true},
		{name: "pagination", row: "reserved-pkg-stems", sqlName: "pagination", stem: true},
		{name: "connection", row: "reserved-pkg-stems", sqlName: "connection", stem: true},
		{name: "shared_types", row: "reserved-pkg-stems", sqlName: "shared_types", stem: true},
		{name: "sets", row: "reserved-pkg-stems", sqlName: "sets", stem: true},
		{name: "cache", row: "reserved-pkg-stems", sqlName: "cache", stem: true},
		{name: "event_hooks", row: "reserved-pkg-stems", sqlName: "event_hooks", stem: true},
		{name: "api_envelopes", row: "reserved-pkg-stems", sqlName: "api_envelopes", stem: true},
		{name: "manifest_embed", row: "reserved-pkg-stems", sqlName: "manifest_embed", stem: true},
		{name: "output.client.file", row: "reserved-pkg-stems", sqlName: "gateway", stem: true, clientFile: "gateway_gen.go"},
		{name: "output.enums.file", row: "reserved-pkg-stems", sqlName: "catalog", stem: true, enumsFile: "catalog_gen.go"},
		{name: "output.types.file", row: "reserved-pkg-stems", sqlName: "shapes", stem: true, typesFile: "shapes_gen.go"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.stem {
				t.Run("table", func(t *testing.T) {
					cfg := rpmConfig(t)
					if tt.clientFile != "" {
						cfg.Output.Client = &config.ClientOutputConfig{File: tt.clientFile}
					}
					if tt.enumsFile != "" {
						cfg.Output.Enums = &config.EnumOutputConfig{File: tt.enumsFile}
					}
					if tt.typesFile != "" {
						cfg.Output.Types = &config.TypeOutputConfig{File: tt.typesFile}
					}
					cfg.Tables["widgets"] = config.TableConfig{StructName: rpmPascal(tt.sqlName)}
					schema := &parser.Schema{Tables: []parser.Table{namedTable("widgets", "")}}

					rpmWantRejected(t, schema, cfg, "table widgets", `"`+tt.sqlName+`"`)
				})
				return
			}

			t.Run("table", func(t *testing.T) {
				cfg := rpmConfig(t)
				cfg.Tables["widgets"] = config.TableConfig{StructName: tt.name}
				schema := &parser.Schema{Tables: []parser.Table{namedTable("widgets", "")}}

				rpmWantRejected(t, schema, cfg, "table widgets", `"`+tt.name+`"`)
			})
			t.Run("enum", func(t *testing.T) {
				cfg := rpmConfig(t)
				if tt.enumStructName != "" {
					cfg.Enums = map[string]config.EnumConfig{tt.sqlName: {StructName: tt.enumStructName}}
				}
				if got := gen.EnumGoTypeName(cfg, tt.sqlName, "", nil); got != tt.name {
					t.Fatalf("fixture: enum %q resolves to %q, want %q", tt.sqlName, got, tt.name)
				}
				schema := &parser.Schema{
					Tables: []parser.Table{namedTable("widgets", "")},
					Enums:  []parser.Enum{{Name: tt.sqlName, Values: []string{"a", "b"}}},
				}

				rpmWantRejected(t, schema, cfg, "enum "+tt.sqlName, `"`+tt.name+`"`)
			})
			t.Run("composite", func(t *testing.T) {
				cfg := rpmConfig(t)
				schema := &parser.Schema{
					Tables: []parser.Table{namedTable("widgets", "")},
					CompositeTypes: []parser.CompositeType{{
						Name:       tt.sqlName,
						Attributes: []parser.Attribute{{Name: "label", Type: "text"}},
					}},
				}

				rpmWantRejected(t, schema, cfg, "composite type "+tt.sqlName, `"`+tt.name+`"`)
			})
			t.Run("domain", func(t *testing.T) {
				cfg := rpmConfig(t)
				schema := &parser.Schema{
					Tables:      []parser.Table{namedTable("widgets", "")},
					DomainTypes: []parser.DomainType{{Name: tt.sqlName, BaseType: "text"}},
				}

				rpmWantRejected(t, schema, cfg, "domain type "+tt.sqlName, `"`+tt.name+`"`)
			})
		})
	}
}

// rpmConfig is nameRuleConfig with the cache and event-hook features asserted
// off, so a hit on one of their names shows the reservation is unconditional.
func rpmConfig(t *testing.T) *config.RootConfig {
	t.Helper()
	cfg := nameRuleConfig()
	if cfg.Cache != nil || cfg.Events != nil {
		t.Fatalf("fixture config unexpectedly enables cache (%v) or events (%v)", cfg.Cache, cfg.Events)
	}
	return cfg
}

// rpmPascal spells a snake_case stem as the struct_name that resolves back to
// it, with the one acronym the stem list needs.
func rpmPascal(snake string) string {
	var b strings.Builder
	for word := range strings.SplitSeq(snake, "_") {
		if word == "api" {
			b.WriteString("API")
			continue
		}
		b.WriteString(strings.ToUpper(word[:1]) + word[1:])
	}
	return b.String()
}

// rpmWantRejected asserts ValidateGeneration rejects the schema with an error
// that names the entity and the reserved name.
func rpmWantRejected(t *testing.T, schema *parser.Schema, cfg *config.RootConfig, entity, reserved string) {
	t.Helper()
	_, err := gen.ValidateGeneration(schema, cfg)
	if err == nil {
		t.Fatalf("ValidateGeneration() error = nil, want %s rejected on the reserved name %s", entity, reserved)
	}
	for _, want := range []string{entity, reserved} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ValidateGeneration() error = %v, want it to mention %q", err, want)
		}
	}
}
