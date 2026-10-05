package gen

// APIGoFieldOverrides returns the Go field-name overrides sqlgen hands gqlgen,
// keyed by GraphQL type name and then by GraphQL field name. The wrapper
// merges them into the temp gqlgen.yml as
// `models.<Type>.fields.<field>.fieldName` (PRD §26.5.6 "Go field naming").
//
// This is what keeps sqlgen from predicting gqlgen's names. Every Go identifier sqlgen's
// generated code references on a gqlgen-*generated* type is listed here, so
// sqlgen never has to predict what gqlgen's `templates.ToGo` would produce
// from the emitted GraphQL name — gqlgen's modelgen discards its own ToGo
// result when an override is present (`plugin/modelgen/models.go`). Prediction
// was the whole defect: the two engines disagreed on acronym sets, and
// camelization is lossy at boundaries neither list can describe (`line2_id` →
// sqlgen "Line2ID", gqlgen "Line2id").
//
// Coverage is deliberately exhaustive over the referenced surface:
//
//   - <T>Filter, Create<T>Input, Update<T>Input — one entry per column field,
//     since the filter and input translators dereference them by Go name.
//   - Update<T>Input's `_inc` / `_dec` operator pair, whose Go names are a
//     literal suffix the resolver template writes verbatim.
//
// Query and Mutation root fields get no entry. gqlgen names root resolvers
// itself and never reads `fieldName` there (`codegen/field.go` returns at
// `case obj.Root:` before the override is consulted), so an entry would
// change nothing. A root field gqlgen spells differently from sqlgen's seed
// is caught after gqlgen runs instead: the wrapper fails generation on the
// panic stub it leaves.
//
// Field names sqlgen writes as literals rather than deriving from a column —
// comparator operands (`eq`, `isNull`, …), `<T>Sort`'s `field` / `direction`,
// the filter's `and` / `or`, and the connection envelope — are deliberately
// absent. Each is a plain lowercase word that gqlgen's default naming already
// renders the way the templates spell it, and none can acquire an acronym,
// because none is derived from user input.
//
// Types are only listed when sqlgen's schema actually emits them, with one
// exception. A bound type (the row struct, the connection envelope) normally
// needs no entry, because gqlgen matches its fields against the consumer's own
// struct with a case-insensitive comparison against the name it derives from
// the GraphQL field — which is the field name sqlgen generated. That premise
// holds only while the Go field *is* derived from the column: a
// `column_map.<col>.name` override breaks the match (GraphQL `ipAddr` looks
// for `IpAddr`, never finds `ClientIP`), so the row type gets an entry for
// exactly those columns and no others. gqlgen honours `fieldName`
// for bound types too — it directs `findBindTarget`'s search
// (`codegen/field.go`).
//
// The same entry carries a second key for a create/update input field whose Go
// TYPE sqlgen dictates (`models.<Input>.fields.<f>.type`) — see
// APIModelField.GoType and dictatedInputGoType.
func APIGoFieldOverrides(apiCtx *APIContext) map[string]map[string]APIModelField {
	if apiCtx == nil {
		return nil
	}

	out := make(map[string]map[string]APIModelField)
	// The two setters write different keys of the SAME entry, so a field that
	// needs both (a numeric array: a Go name and a dictated Go type) produces
	// one node rather than two that gqlgen would read independently.
	fieldsFor := func(typeName string) map[string]APIModelField {
		fields, ok := out[typeName]
		if !ok {
			fields = make(map[string]APIModelField)
			out[typeName] = fields
		}
		return fields
	}
	set := func(typeName, graphQLName, goName string) {
		if typeName == "" || graphQLName == "" || goName == "" {
			return
		}
		fields := fieldsFor(typeName)
		entry := fields[graphQLName]
		entry.FieldName = goName
		fields[graphQLName] = entry
	}
	setType := func(typeName, graphQLName, goType string) {
		if typeName == "" || graphQLName == "" || goType == "" {
			return
		}
		fields := fieldsFor(typeName)
		entry := fields[graphQLName]
		entry.GoType = goType
		fields[graphQLName] = entry
	}

	for _, t := range apiCtx.Tables {
		tableGoFieldOverrides(t, set, setType)
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

// APIModelField is one `models.<Type>.fields.<field>` entry sqlgen dictates to
// gqlgen. Both keys are optional and independent — most entries carry only
// FieldName; a numeric-array input field carries both.
type APIModelField struct {
	// FieldName is the Go identifier gqlgen must give the field
	// (`fieldName:`), so sqlgen's templates can reference it by name instead
	// of predicting gqlgen's camelizer.
	FieldName string
	// GoType is the Go type gqlgen must give the field on a type it
	// GENERATES (`type:`), overriding its own `Model[0]` binding. Set only
	// for a create/update input field whose model type gqlgen would otherwise
	// disagree with irreconcilably — see dictatedInputGoType.
	GoType string
}

// tableGoFieldOverrides emits one table's field overrides through set (Go
// names) and setType (dictated Go types). Split out of APIGoFieldOverrides so
// each surface stays individually readable.
func tableGoFieldOverrides(t APITableContext, set func(typeName, graphQLName, goName string), setType func(typeName, graphQLName, goType string)) {
	// The row type is bound to sqlgen's own struct, so only a renamed field
	// needs to be stated — see the exception in APIGoFieldOverrides's doc.
	// Gated on Readable for the same reason the schema template is: a column
	// whose access role drops it from the object type has no GraphQL field
	// for the entry to attach to (§32.2).
	for _, f := range t.Fields {
		if f.GoFieldOverridden && f.Readable {
			set(t.StructName, f.GraphQLName, f.GoFieldName)
		}
	}
	for _, f := range t.FilterFields {
		set(t.StructName+"Filter", f.GraphQLName, f.GoFieldName)
	}
	// Relationship members sit on the same input and are dereferenced by Go
	// name in the filter translator, so they need the identical treatment —
	// a relationship named `order_items` camelizes to `orderItems`, and the
	// two engines' capitalizers are not obliged to agree on what that becomes
	// in Go.
	for _, r := range t.FilterRelationships {
		set(t.StructName+"Filter", r.GraphQLName, r.GoFieldName)
	}
	if t.HasCreateInput {
		createInput := "Create" + t.StructName + "Input"
		for _, f := range t.CreateInputFields {
			set(createInput, f.GraphQLName, f.GoFieldName)
			setType(createInput, f.GraphQLName, f.GqlgenGoType)
		}
	}
	nestedGoFieldOverrides(t.Nested, set, setType)
	if !t.HasUpdateInput {
		return
	}
	updateInput := "Update" + t.StructName + "Input"
	for _, f := range t.UpdateInputFields {
		set(updateInput, f.GraphQLName, f.GoFieldName)
		setType(updateInput, f.GraphQLName, f.GqlgenGoType)
	}
	// The paired operators live on the update input alongside the plain set
	// fields (§26.5.4).
	for _, op := range t.UpdateOps {
		set(updateInput, op.IncGraphQLName, op.IncGoField)
		set(updateInput, op.DecGraphQLName, op.DecGoField)
	}
}

// nestedGoFieldOverrides emits the overrides for the nested-mutation input
// types (PRD §26.5.1). The translators dereference three kinds of member by Go
// name, and every one is derived from something the consumer wrote:
//
//   - the wrapper's flat-input member, named after the table (`user` → `User`);
//   - each edge member, named after the relationship (`draftChildren` →
//     `DraftChildren`), which is also the model wrapper's field;
//   - each nested child input's members, which are the target's create-input
//     columns and take exactly the entries the target's Create<T>Input does,
//     dictated Go types included.
//
// The verb members — `create`, `connect`, `disconnect`, `clear` — are literal
// lowercase words and take none, on the rule APIGoFieldOverrides states for
// every other literal.
func nestedGoFieldOverrides(n *APINestedContext, set func(typeName, graphQLName, goName string), setType func(typeName, graphQLName, goType string)) {
	if n == nil {
		return
	}
	wrappers := make([]string, 0, 3)
	if n.EmitCreate {
		wrappers = append(wrappers, n.CreateInputName)
	}
	if n.EmitUpdate {
		wrappers = append(wrappers, n.UpdateInputName)
	}
	if n.EmitUpsert {
		wrappers = append(wrappers, n.UpsertInputName)
	}
	for _, w := range wrappers {
		set(w, n.ParentGraphQLName, n.ParentFieldName)
	}
	for _, e := range n.Edges {
		if e.EmitCreateBlock {
			set(n.CreateInputName, e.GraphQLName, e.FieldName)
		}
		if e.EmitUpdateBlock {
			if n.EmitUpdate {
				set(n.UpdateInputName, e.GraphQLName, e.FieldName)
			}
			if n.EmitUpsert {
				set(n.UpsertInputName, e.GraphQLName, e.FieldName)
			}
		}
		for _, f := range e.ChildFields {
			set(e.ChildInputName, f.Input.GraphQLName, f.Input.GoFieldName)
			setType(e.ChildInputName, f.Input.GraphQLName, f.Input.GqlgenGoType)
		}
	}
}
