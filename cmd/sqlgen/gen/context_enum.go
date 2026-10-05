package gen

import (
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
