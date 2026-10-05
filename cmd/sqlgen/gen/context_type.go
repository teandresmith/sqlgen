package gen

import (
	"slices"
	"strings"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// BuildTypeContext builds TypeContext from the parsed schema and config.
func BuildTypeContext(input *GenerateInput, collisions map[string]bool) TypeContext {
	return TypeContext{
		Composites: buildCompositeContexts(input, collisions),
		Domains:    buildDomainContexts(input, collisions),
		Extras:     buildExtraContexts(input.Config),
	}
}

func buildCompositeContexts(input *GenerateInput, _ map[string]bool) []CompositeTypeContext {
	contexts := make([]CompositeTypeContext, 0, len(input.Schema.CompositeTypes))
	for _, ct := range input.Schema.CompositeTypes {
		fields := make([]CompositeFieldContext, 0, len(ct.Attributes))
		for _, attr := range ct.Attributes {
			gt := input.Resolver.Resolve(attr.Type, false, attr.Name, nil, nil)
			fields = append(fields, CompositeFieldContext{
				FieldName: FieldName(attr.Name),
				GoType:    gt.Name,
				Import:    gt.Import,
				JSONTag:   attr.Name,
			})
		}
		contexts = append(contexts, CompositeTypeContext{
			Name:       ct.Name,
			Schema:     ct.Schema,
			GoTypeName: toPascalCase(ct.Name),
			Fields:     fields,
			DocComment: ct.Comment,
		})
	}
	slices.SortFunc(contexts, func(a, b CompositeTypeContext) int {
		if c := strings.Compare(a.Schema, b.Schema); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
	return contexts
}

func buildDomainContexts(input *GenerateInput, _ map[string]bool) []DomainTypeContext {
	contexts := make([]DomainTypeContext, 0, len(input.Schema.DomainTypes))
	for _, dt := range input.Schema.DomainTypes {
		gt := input.Resolver.Resolve(dt.BaseType, false, "", nil, nil)
		contexts = append(contexts, DomainTypeContext{
			Name:       dt.Name,
			Schema:     dt.Schema,
			GoTypeName: toPascalCase(dt.Name),
			BaseGoType: gt.Name,
			Import:     gt.Import,
			DocComment: dt.Comment,
		})
	}
	slices.SortFunc(contexts, func(a, b DomainTypeContext) int {
		if c := strings.Compare(a.Schema, b.Schema); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
	return contexts
}

func buildExtraContexts(cfg *config.RootConfig) []ExtraTypeContext {
	// Convert map to sorted slice.
	names := make([]string, 0, len(cfg.Extras))
	for name := range cfg.Extras {
		names = append(names, name)
	}
	slices.Sort(names)

	contexts := make([]ExtraTypeContext, 0, len(names))
	for _, name := range names {
		extra := cfg.Extras[name]

		// Sort field names.
		fieldNames := make([]string, 0, len(extra.Fields))
		for fn := range extra.Fields {
			fieldNames = append(fieldNames, fn)
		}
		slices.Sort(fieldNames)

		fields := make([]ExtraFieldContext, 0, len(fieldNames))
		for _, fn := range fieldNames {
			f := extra.Fields[fn]

			// Sort tags.
			tagKeys := make([]string, 0, len(f.Tags))
			for k := range f.Tags {
				tagKeys = append(tagKeys, k)
			}
			slices.Sort(tagKeys)
			tags := make([]TagPair, 0, len(tagKeys))
			for _, k := range tagKeys {
				tags = append(tags, TagPair{Key: k, Value: f.Tags[k]})
			}

			fields = append(fields, ExtraFieldContext{
				FieldName:   FieldName(fn),
				GoType:      f.Type,
				Import:      f.Import,
				Description: f.Description,
				Tags:        tags,
			})
		}

		contexts = append(contexts, ExtraTypeContext{
			GoTypeName:  toPascalCase(name),
			Description: extra.Description,
			Fields:      fields,
		})
	}
	return contexts
}
