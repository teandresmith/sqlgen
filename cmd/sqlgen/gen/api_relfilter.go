package gen

import (
	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// apiExposedTargets returns the set of entities a relationship may reach on the
// GraphQL surface — as a `<T>Filter` member or as an object-type field — keyed
// by Go struct name.
//
// A table is a member when it is API-enabled (`api.enabled` — per-table
// override, else global) and at least one of its columns survives §32.2 as
// readable. Both halves are the gate PRD §26.5.3 states for the filter member —
// "a relationship whose target entity is excluded from the API (`api.enabled:
// false`, or every column dropped by §32.2) emits no filter member" — because
// otherwise the filter would be a read side-channel into an entity the schema
// deliberately hides: `EXISTS (SELECT 1 FROM secrets …)` answers a yes/no
// question about rows the caller cannot select.
//
// The object-type field follows the same rule (PRD §26.10): a field
// typed as a hidden entity names a type the schema never declares, and gqlgen
// rejects the whole document.
//
// A view is a member when it is API-enabled. A declared relationship may point
// at one, and every view column is public (a view has no column_map), so
// `api.enabled` alone decides. Filter members are unaffected: the model side
// builds them for table targets only.
//
// The key is the struct name because that is what a relationship field
// renders as its type, and it is unique project-wide (§8.5).
//
// It is computed once for the whole project rather than per table, because the
// answer is a property of the target and every parent asks the same question
// about it.
func apiExposedTargets(cfg *config.RootConfig, tables []TableContext, views []ViewContext) map[string]bool {
	out := make(map[string]bool, len(tables)+len(views))
	for i := range tables {
		tc := &tables[i]
		if !tableAPIEnabled(cfg, tc.TableName, tc.Schema) {
			continue
		}
		if !hasAPIReadableColumn(tc.Columns) {
			continue
		}
		out[tc.StructName] = true
	}
	for i := range views {
		if viewAPIEnabled(cfg, views[i].ViewName, views[i].Schema) {
			out[views[i].StructName] = true
		}
	}
	return out
}

// hasAPIReadableColumn reports whether any column survives §32.2 with the
// read capability. A table where none does emits an object type with no
// fields, so there is nothing for a nested filter to select against.
func hasAPIReadableColumn(cols []ColumnContext) bool {
	for _, col := range cols {
		if columnAccessCapabilities(col).APIReadable {
			return true
		}
	}
	return false
}

// buildAPIFilterRelationships projects the model-side relationship filter
// members onto the GraphQL `<T>Filter` input (PRD §26.4, §26.5.3).
//
// The model side already decided WHICH relationships can be compiled into a
// correlated EXISTS (context_relfilter.go — list relationships only, single
// correlation column, target inside the generated set). This function only
// decides which of those the API may expose, so a member can never reach the
// schema without a `ToConditions` arm behind it.
//
// `filterFieldNames` carries the GraphQL names the column half of the input
// already claimed. A relationship whose name camelizes onto one of them is
// dropped rather than emitted twice, with "the column wins" — the same tiebreak
// the model struct applies (context_relfilter.go).
//
// That guard is defensive rather than load-bearing today: the model side
// already refuses a relationship whose GO field name collides with a filterable
// column's, and both GraphQL names derive from the same two SQL names, so a
// collision here implies one there. It sits at this point anyway because this
// is where an invalid document would be emitted — two fields of one name make
// gqlgen reject the whole schema, which is a long way from the config line that
// caused it.
func buildAPIFilterRelationships(rfs []RelationshipFilterContext, casing string, targets map[string]bool, filterFieldNames map[string]bool) []APIFilterRelationship {
	if len(rfs) == 0 {
		return nil
	}
	out := make([]APIFilterRelationship, 0, len(rfs))
	taken := make(map[string]bool, len(rfs))
	for _, rf := range rfs {
		if !targets[rf.TargetStructName] {
			continue
		}
		name := graphQLFieldName(rf.SQLName, casing)
		if filterFieldNames[name] || taken[name] {
			continue
		}
		taken[name] = true
		out = append(out, APIFilterRelationship{
			GraphQLName:    name,
			GoFieldName:    rf.FieldName,
			ModelFieldName: rf.FieldName,
			InputTypeName:  rf.TargetStructName + "Filter",
			TranslatorFunc: "translate" + rf.TargetStructName + "Filter",
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// filterFieldNameSet indexes the GraphQL names the column half of a filter
// input emits, so the relationship half can avoid colliding with one.
func filterFieldNameSet(fields []APIFilterField) map[string]bool {
	out := make(map[string]bool, len(fields))
	for _, f := range fields {
		out[f.GraphQLName] = true
	}
	return out
}
