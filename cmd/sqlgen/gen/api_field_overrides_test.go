package gen

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

// overrideFixture builds an APIContext covering every surface
// APIGoFieldOverrides must reach: a filter field, both input types, an
// increment pair, and the full root-operation set.
func overrideFixture() *APIContext {
	return &APIContext{
		Tables: []APITableContext{{
			StructName:       "CSVRecord",
			StructNamePlural: "CSVRecords",
			QueryName:        "csvRecord",
			QueryNamePlural:  "csvRecords",
			ListQueryName:    "csvRecordList",
			HasCreateInput:   true,
			HasUpdateInput:   true,
			HasConflictPK:    true,
			FilterFields: []APIFilterField{
				{GraphQLName: "line2ID", GoFieldName: "Line2ID"},
				{GraphQLName: "mac", GoFieldName: "MAC"},
			},
			CreateInputFields: []APIInputField{
				{GraphQLName: "line2ID", GoFieldName: "Line2ID"},
			},
			UpdateInputFields: []APIInputField{
				{GraphQLName: "stock", GoFieldName: "Stock"},
			},
			UpdateOps: []APIUpdateOp{{
				SQLName:        "stock",
				IncGraphQLName: "stock_inc",
				DecGraphQLName: "stock_dec",
				SetGoField:     "Stock",
				IncGoField:     "StockInc",
				DecGoField:     "StockDec",
			}},
			Operations: ResolvedOperations{
				Get: true, Connection: true, Paginate: true,
				Create: true, Update: true, Upsert: true, HardDelete: true,
			},
		}},
	}
}

func TestAPIGoFieldOverrides(t *testing.T) {
	got := APIGoFieldOverrides(overrideFixture())

	want := map[string]map[string]APIModelField{
		"CSVRecordFilter": {
			"line2ID": {FieldName: "Line2ID"},
			"mac":     {FieldName: "MAC"},
		},
		"CreateCSVRecordInput": {
			"line2ID": {FieldName: "Line2ID"},
		},
		"UpdateCSVRecordInput": {
			"stock":     {FieldName: "Stock"},
			"stock_inc": {FieldName: "StockInc"},
			"stock_dec": {FieldName: "StockDec"},
		},
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("APIGoFieldOverrides mismatch (-want +got):\n%s", diff)
	}
}

// TestAPIGoFieldOverrides_CoversEveryReferencedName is the completeness guard.
// Generated code dereferences these identifiers on gqlgen-generated types, so
// any one sqlgen fails to dictate falls back to gqlgen's own capitalizer — and
// a disagreement there does not degrade gracefully, it fails to compile
// Asserting coverage from the context rather than from a fixed list
// means a new referenced field cannot be added without either being covered or
// failing here.
func TestAPIGoFieldOverrides_CoversEveryReferencedName(t *testing.T) {
	ctx := overrideFixture()
	got := APIGoFieldOverrides(ctx)

	has := func(typeName, graphQLName, goName string) {
		t.Helper()
		fields, ok := got[typeName]
		if !ok {
			t.Errorf("no overrides emitted for type %s", typeName)
			return
		}
		if fields[graphQLName].FieldName != goName {
			t.Errorf("%s.%s fieldName = %q, want %q", typeName, graphQLName, fields[graphQLName].FieldName, goName)
		}
	}

	for _, tbl := range ctx.Tables {
		for _, f := range tbl.FilterFields {
			has(tbl.StructName+"Filter", f.GraphQLName, f.GoFieldName)
		}
		for _, f := range tbl.CreateInputFields {
			has("Create"+tbl.StructName+"Input", f.GraphQLName, f.GoFieldName)
		}
		for _, f := range tbl.UpdateInputFields {
			has("Update"+tbl.StructName+"Input", f.GraphQLName, f.GoFieldName)
		}
		for _, op := range tbl.UpdateOps {
			has("Update"+tbl.StructName+"Input", op.IncGraphQLName, op.IncGoField)
			has("Update"+tbl.StructName+"Input", op.DecGraphQLName, op.DecGoField)
		}
	}

	// Root fields are deliberately absent: gqlgen never reads fieldName on
	// Query / Mutation, so an entry there is inert. The wrapper's
	// post-gqlgen stub check guards those names instead.
	queries, mutations := CollectSqlgenRootFields(ctx)
	if len(queries) == 0 || len(mutations) == 0 {
		t.Fatal("fixture produced no root fields; the absence assertion below would be vacuous")
	}
	for _, root := range []string{"Query", "Mutation"} {
		if fields, ok := got[root]; ok {
			t.Errorf("%s carries fieldName entries gqlgen ignores on root fields: %v", root, fields)
		}
	}
}

// TestAPIGoFieldOverrides_RootFieldPairsStayInStep pins that the Go name the
// seed template declares and the GraphQL name the schema template emits come
// from one gated source. Deriving them separately is how the sort surface's
// predecessor drifted; here a mismatch would mean the seed does not satisfy
// gqlgen's resolver interface.
func TestAPIGoFieldOverrides_RootFieldPairsStayInStep(t *testing.T) {
	ctx := overrideFixture()
	queries, mutations := CollectSqlgenRootFields(ctx)
	gotQueries, gotMutations := CollectSqlgenManagedFields(ctx)

	if diff := cmp.Diff(goNames(queries), gotQueries); diff != "" {
		t.Errorf("query managed-field names diverged from the paired source (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(goNames(mutations), gotMutations); diff != "" {
		t.Errorf("mutation managed-field names diverged from the paired source (-want +got):\n%s", diff)
	}
}

// TestAPIGoFieldOverrides_BoundRowType pins the one case where a *bound* type
// gets an entry. The row type binds to sqlgen's own struct and gqlgen matches
// its fields case-insensitively against the name derived from the GraphQL
// field, so an un-renamed column needs nothing — but a
// `column_map.<col>.name` override breaks that match and has to be stated.
func TestAPIGoFieldOverrides_BoundRowType(t *testing.T) {
	tests := []struct {
		name string
		// fields is the row type's GraphQL field list.
		fields []APIFieldContext
		want   map[string]APIModelField
	}{
		{
			name: "no override emits no row-type entry",
			fields: []APIFieldContext{
				{GraphQLName: "ipAddr", GoFieldName: "IPAddr", Readable: true},
			},
		},
		{
			name: "override emits an entry for that field only",
			fields: []APIFieldContext{
				{GraphQLName: "ipAddr", GoFieldName: "ClientIP", GoFieldOverridden: true, Readable: true},
				{GraphQLName: "mac", GoFieldName: "MAC", Readable: true},
			},
			want: map[string]APIModelField{"ipAddr": {FieldName: "ClientIP"}},
		},
		{
			// An access role that drops the column from the object type
			// leaves no GraphQL field for the entry to attach to.
			name: "override on a non-readable column emits nothing",
			fields: []APIFieldContext{
				{GraphQLName: "internalScore", GoFieldName: "Score", GoFieldOverridden: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := overrideFixture()
			ctx.Tables[0].Fields = tt.fields

			got := APIGoFieldOverrides(ctx)[ctx.Tables[0].StructName]
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("row-type overrides mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
