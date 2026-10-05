package gen

import (
	"slices"
	"strings"

	"github.com/teandresmith/sqlgen/parser"
)

// BuildSetContexts builds SetContext values from the parsed schema.
func BuildSetContexts(schema *parser.Schema, collisions map[string]bool) []SetContext {
	contexts := make([]SetContext, 0, len(schema.Sets))
	for _, s := range schema.Sets {
		goTypeName := StructName(s.Name, s.Schema, collisions)
		contexts = append(contexts, SetContext{
			Name:            s.Name,
			Schema:          s.Schema,
			GoTypeName:      goTypeName,
			ValueGoTypeName: goTypeName + "Value",
			Values:          s.Values,
			DocComment:      s.Comment,
		})
	}
	slices.SortFunc(contexts, func(a, b SetContext) int {
		if c := strings.Compare(a.Schema, b.Schema); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
	return contexts
}
