package gen

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/parser"
)

// EnumConfigFor resolves the `enums:` entry for one schema enum, trying the
// schema-qualified key before the bare one — the same two-step lookup
// TableConfig resolution uses (PRD §4.11, §5.5). The zero value means the enum
// has no config entry.
func EnumConfigFor(cfg *config.RootConfig, name, schema string) config.EnumConfig {
	if cfg == nil {
		return config.EnumConfig{}
	}
	if ec, ok := cfg.Enums[qualifiedTableName(schema, name)]; ok {
		return ec
	}
	return cfg.Enums[name]
}

// unmatchedEnumKeyWarnings reports every `enums:` key that EnumConfigFor
// resolves for no schema enum, so the entry has no effect (PRD §4.11, §4.13).
// A key matches an enum by the same two spellings that lookup tries: the
// schema-qualified name or the bare one.
//
// A warning rather than an error, the unusedScalarWarnings precedent: a
// schema narrowed by `introspect.schemas` or `introspect.tables` can
// legitimately leave a key unmatched. What it catches is a typo, or a MySQL
// key in the `<table>_<column>_enum` form rather than `<table>__<column>_enum`
// (PRD §8.3). A key naming a MySQL SET type gets its own message,
// since a SET takes no override at all.
//
// Emitted by buildAllContexts (`generate`) and ValidateGeneration
// (`validate`), not by BuildEntityContextsFromSchema: `graphql gen` runs
// chained inside `generate`, so warning there too would report each key twice.
func unmatchedEnumKeyWarnings(cfg *config.RootConfig, schema *parser.Schema) []string {
	if cfg == nil || len(cfg.Enums) == 0 {
		return nil
	}
	enums := make(map[string]bool, 2*len(schema.Enums))
	for _, e := range schema.Enums {
		enums[qualifiedTableName(e.Schema, e.Name)] = true
		enums[e.Name] = true
	}
	sets := make(map[string]bool, 2*len(schema.Sets))
	for _, s := range schema.Sets {
		sets[qualifiedTableName(s.Schema, s.Name)] = true
		sets[s.Name] = true
	}
	var out []string
	for _, key := range slices.Sorted(maps.Keys(cfg.Enums)) {
		switch {
		case enums[key]:
		case sets[key]:
			out = append(out, fmt.Sprintf(
				"enums.%s: %s is a SET type, which takes no enums: override, so the entry has no effect — rename the table or column to change its Go type name",
				key, key,
			))
		default:
			msg := fmt.Sprintf("enums.%s: matches no enum in the schema, so the entry has no effect — check the spelling", key)
			if cfg.Input.Dialect == config.DialectMySQL {
				msg += " (a MySQL inline ENUM is named <table>__<column>_enum)"
			}
			out = append(out, msg)
		}
	}
	return out
}

// EnumGoTypeName returns the Go type name one schema enum resolves to,
// honouring an `enums.<name>.struct_name` override (PRD §4.11).
//
// This is the single derivation of that name, and it is exported because two
// callers need it and must not disagree: BuildEnumContexts, which names the
// generated type, and registerSchemaTypes, which tells the gotype resolver
// what an enum COLUMN resolves to. A column resolving to one spelling while
// the type is generated under another produces a models package that does not
// compile, so the two read from here rather than each calling StructName.
func EnumGoTypeName(cfg *config.RootConfig, name, schema string, collisions map[string]bool) string {
	return structNameFor(name, schema, EnumConfigFor(cfg, name, schema).StructName, collisions)
}

// BuildEnumContexts builds EnumContext values from the parsed schema.
// When the dialect is "postgres", each enum gets a SliceGoTypeName for array
// column support.
func BuildEnumContexts(schema *parser.Schema, collisions map[string]bool, cfg *config.RootConfig) []EnumContext {
	contexts := make([]EnumContext, 0, len(schema.Enums))
	for _, e := range schema.Enums {
		enumCfg := EnumConfigFor(cfg, e.Name, e.Schema)
		goTypeName := structNameFor(e.Name, e.Schema, enumCfg.StructName, collisions)
		// A config description outranks the SQL COMMENT ON TYPE: the consumer
		// wrote it against the generated Go type, whereas the comment
		// describes the database object (PRD §4.11).
		doc := e.Comment
		if enumCfg.Description != "" {
			doc = enumCfg.Description
		}
		ctx := EnumContext{
			Name:       e.Name,
			Schema:     e.Schema,
			GoTypeName: goTypeName,
			Values:     e.Values,
			DocComment: doc,
		}
		if cfg != nil && cfg.Input.Dialect == config.DialectPostgres {
			ctx.SliceGoTypeName = goTypeName + "Slice"
		}
		contexts = append(contexts, ctx)
	}
	slices.SortFunc(contexts, func(a, b EnumContext) int {
		if c := strings.Compare(a.Schema, b.Schema); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
	return contexts
}
