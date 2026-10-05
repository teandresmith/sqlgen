package gen

import (
	"errors"
	"fmt"
)

// ValidateAPIFilterCompleteness enforces the "one field map, two emitters"
// invariant on the filter surface (PRD §26.5.3): the
// `input <T>Filter` block the schema template emits and the body the per-table
// filter translator emits MUST name the same set of fields.
//
// The failure it forbids is silent, which is why it is a hard codegen error
// rather than a warning. A field present in the schema but absent from the
// translator is accepted at parse time and dropped at execution time — the
// server answers with unfiltered rows while the client believes its argument
// took effect. A field present in the translator but absent from the schema is
// the mirror: the generated body reads `in.<Field>` off a gqlgen input type
// that has no such member, so the graph package does not compile.
//
// It checks both halves of the input:
//
//   - Columns. Schema side is `Fields` under the template's own gate
//     (`not $f.PrimaryKey` and `$f.Filterable`); translator side is
//     `FilterFields`.
//   - Relationship members (§26.4, §11.1). Both emitters read one
//     `FilterRelationships` slice, so the drift a column pair can suffer is
//     structurally unrepresentable here; what is checkable is that each entry
//     can render BOTH halves, that it names a member the model filter struct
//     actually has, and that it does not collide with a column field in the
//     same input block.
//
// Columns that are deliberately filterable on neither side — `json[]` /
// `jsonb[]`, a `types.JSON` column on SQLite, a column
// whose access role drops the filter surface (§32.2), and a comparator with no
// sound GraphQL projection (resolveAPIFilterProjection) — satisfy this lint by
// being absent from both, not by being exempt from it.
//
// It takes an APIEntity rather than a TableContext so a view is linted by this
// exact code path with no special case, the same way the §26.5.2 walker lint
// is. A view carries no relationship filters, so the relationship loop simply
// has nothing to check for it.
//
// Every violation is reported, not just the first, so a context built wrong in
// several places is diagnosed in one pass.
func ValidateAPIFilterCompleteness(src APIEntity, apiTable APITableContext) error {
	var errs []error

	// The schema side, derived under the exact gate schema.graphqls.tmpl
	// applies to the `input <T>Filter` block.
	schemaCols := make(map[string]APIFieldContext, len(apiTable.Fields))
	for _, f := range apiTable.Fields {
		if f.PrimaryKey || !f.Filterable {
			continue
		}
		schemaCols[f.GraphQLName] = f
	}

	translated := make(map[string]bool, len(apiTable.FilterFields))
	for _, ff := range apiTable.FilterFields {
		translated[ff.GraphQLName] = true
	}

	// Direction 1 — schema field with no translator entry (the silent-drop
	// case). Iterating Fields rather than the map keeps the report in column
	// order.
	for _, f := range apiTable.Fields {
		if f.PrimaryKey || !f.Filterable {
			continue
		}
		// Gated INTO the input block with no input type resolved renders
		// `name: ` — an invalid document, the filter twin of the empty
		// sort-enum value guarded below. Unreachable while Filterable is read
		// back off the projection (resolveAPIFilterProjection), which is
		// precisely why it is worth stating: this is the assertion that keeps
		// the two from becoming separate facts again.
		if f.Filter.InputTypeName == "" {
			errs = append(errs, fmt.Errorf(
				"api: filter for %s %q gates column %q into `input %sFilter` but resolved no comparator input type "+
					"(the field would render with an empty type — PRD §26.5.3)",
				src.Kind, src.Name, f.SQLName, apiTable.StructName,
			))
			continue
		}
		if !translated[f.GraphQLName] {
			errs = append(errs, fmt.Errorf(
				"api: filter for %s %q emits column %q as GraphQL field %q with no entry in the filter translator "+
					"(a schema field the translator cannot dispatch is accepted at parse time and ignored at execution time — PRD §26.5.3)",
				src.Kind, src.Name, f.SQLName, f.GraphQLName,
			))
		}
	}

	// Direction 2 — translator entry with no schema field (a dangling
	// reference to an absent gqlgen input field).
	for _, ff := range apiTable.FilterFields {
		if _, ok := schemaCols[ff.GraphQLName]; !ok {
			errs = append(errs, fmt.Errorf(
				"api: filter translator for %s %q has an entry for column %q (GraphQL field %q) with no field in the emitted `input %sFilter` "+
					"(the generated body would read an absent gqlgen input member — PRD §26.5.3)",
				src.Kind, src.Name, ff.SQLName, ff.GraphQLName, apiTable.StructName,
			))
		}
	}

	errs = append(errs, validateAPIFilterRelationships(src, apiTable, schemaCols)...)
	return errors.Join(errs...)
}

// validateAPIFilterRelationships is the relationship half of the filter lint.
// Split out because it answers a different question from the column half: the
// two emitters read one slice, so the check is not "do the two sides agree"
// but "can this entry render both sides, and is what it renders resolvable".
func validateAPIFilterRelationships(src APIEntity, apiTable APITableContext, schemaCols map[string]APIFieldContext) []error {
	if len(apiTable.FilterRelationships) == 0 {
		return nil
	}

	// The model-side members the translator may assign into. An entry naming
	// anything else emits `out.<Field> = …` against a struct without it.
	modelMembers := make(map[string]bool, len(src.RelationshipFilters))
	for _, rf := range src.RelationshipFilters {
		modelMembers[rf.FieldName] = true
	}

	var errs []error
	seen := make(map[string]bool, len(apiTable.FilterRelationships))
	for _, r := range apiTable.FilterRelationships {
		// Both halves must be renderable. An entry with a schema half and no
		// translator half is exactly the column surface's silent-drop failure,
		// reached through the relationship member instead of a comparator.
		if r.GraphQLName == "" || r.InputTypeName == "" || r.GoFieldName == "" ||
			r.ModelFieldName == "" || r.TranslatorFunc == "" {
			errs = append(errs, fmt.Errorf(
				"api: filter for %s %q has a relationship member that renders only one half "+
					"(graphql=%q input=%q gqlgenField=%q modelField=%q translator=%q — every member must render both the schema field and the translator call, PRD §26.5.3)",
				src.Kind, src.Name, r.GraphQLName, r.InputTypeName, r.GoFieldName, r.ModelFieldName, r.TranslatorFunc,
			))
			continue
		}
		if !modelMembers[r.ModelFieldName] {
			errs = append(errs, fmt.Errorf(
				"api: filter for %s %q emits relationship member %q assigning model field %q, which the model filter struct does not have "+
					"(PRD §11.1 / §26.5.3)",
				src.Kind, src.Name, r.GraphQLName, r.ModelFieldName,
			))
		}
		// A relationship member and a column field land in the same `input
		// <T>Filter` block, so a shared name makes the whole document invalid —
		// a long way from the schema line that caused it. buildAPIFilterRelationships
		// already applies the "the column wins" tiebreak; this is the guard
		// that a future edit cannot drop it silently.
		if _, clash := schemaCols[r.GraphQLName]; clash {
			errs = append(errs, fmt.Errorf(
				"api: filter for %s %q emits both a column field and a relationship member named %q "+
					"(two fields of one name make `input %sFilter` invalid — PRD §26.4)",
				src.Kind, src.Name, r.GraphQLName, apiTable.StructName,
			))
		}
		if seen[r.GraphQLName] {
			errs = append(errs, fmt.Errorf(
				"api: filter for %s %q emits relationship member %q twice "+
					"(two fields of one name make `input %sFilter` invalid — PRD §26.4)",
				src.Kind, src.Name, r.GraphQLName, apiTable.StructName,
			))
		}
		seen[r.GraphQLName] = true
	}
	return errs
}

// ValidateAPISortCompleteness is the sort surface's twin of
// ValidateAPIFilterCompleteness (PRD §26.5.3): the
// `enum <T>SortField` values the schema template emits and the cases the
// generated `<t>SortFieldToColumn` switch carries MUST be the same set.
//
// It makes sort-enum drift on a digit-leading column unlandable. The two
// spellings used to be derived independently — `screamingSnakeCase` in the
// schema (which guards a digit-leading column with a `col_` prefix per §8.5)
// against `strings.ToUpper(toSnakeCase(...))` in the switch (which does not) —
// so a column like `2010_revenue` advertised `COL_2010_REVENUE` and matched
// `case "2010_REVENUE"`. The enum value parses, the switch falls through, and
// the query comes back in an arbitrary order.
//
// It additionally checks that each case returns a column the entity actually
// has: the switch's return value goes straight into `sql.Sort{Column: …}`, so
// a stale name is not a compile error but a SQL error on every sorted read.
func ValidateAPISortCompleteness(src APIEntity, apiTable APITableContext) error {
	var errs []error

	columns := make(map[string]bool, len(src.Columns))
	for _, c := range src.Columns {
		columns[c.Name] = true
	}

	// The schema side, under the exact gate schema.graphqls.tmpl applies to
	// the `enum <T>SortField` block.
	schemaValues := make(map[string]bool, len(apiTable.Fields))
	for _, f := range apiTable.Fields {
		if !f.Sortable {
			continue
		}
		if f.SortEnumValue == "" {
			// The enum block would render a blank line where a value belongs,
			// which gqlgen rejects — and the switch has nothing to case on.
			errs = append(errs, fmt.Errorf(
				"api: sort for %s %q marks column %q sortable but resolved no enum value "+
					"(the `enum %sSortField` block would emit an empty value — PRD §8.5 / §26.5.3)",
				src.Kind, src.Name, f.SQLName, apiTable.StructName,
			))
			continue
		}
		schemaValues[f.SortEnumValue] = true
	}

	cases := make(map[string]bool, len(apiTable.SortFields))
	for _, sf := range apiTable.SortFields {
		cases[sf.EnumValue] = true
	}

	// Direction 1 — enum value with no SortFieldToColumn case. The switch
	// falls through to its default and the read comes back unsorted.
	for _, f := range apiTable.Fields {
		if !f.Sortable || f.SortEnumValue == "" {
			continue
		}
		if !cases[f.SortEnumValue] {
			errs = append(errs, fmt.Errorf(
				"api: sort for %s %q emits enum value %q for column %q with no case in %sSortFieldToColumn "+
					"(the value parses and the switch ignores it, so the read comes back unsorted — PRD §26.5.3)",
				src.Kind, src.Name, f.SortEnumValue, f.SQLName, toCamelCase(apiTable.StructName),
			))
		}
	}

	// Direction 2 — a case with no emitted enum value, plus the column the
	// case resolves to.
	for _, sf := range apiTable.SortFields {
		if !schemaValues[sf.EnumValue] {
			errs = append(errs, fmt.Errorf(
				"api: sort for %s %q has a %sSortFieldToColumn case for %q with no value in the emitted `enum %sSortField` "+
					"(a case nothing can select — PRD §26.5.3)",
				src.Kind, src.Name, toCamelCase(apiTable.StructName), sf.EnumValue, apiTable.StructName,
			))
			continue
		}
		if !columns[sf.SQLColumn] {
			errs = append(errs, fmt.Errorf(
				"api: sort for %s %q maps enum value %q to column %q, which the %s does not have "+
					"(the name goes straight into ORDER BY — PRD §26.5.3)",
				src.Kind, src.Name, sf.EnumValue, sf.SQLColumn, src.Kind,
			))
		}
	}

	return errors.Join(errs...)
}
