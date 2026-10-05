package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// TestMapRelationshipToGraphQL_NoGoTypeSniff pins the type-based dispatch: the
// GraphQL field shape is derived from the first-class RelationshipContext.Type,
// never from the string spelling of GoType. Each case supplies a deliberately
// "wrong" GoType (and, for good measure, a mismatched Side / FKNullable) that
// the old heuristic would have mapped to a different shape, and asserts the
// output tracks Type instead.
func TestMapRelationshipToGraphQL_NoGoTypeSniff(t *testing.T) {
	tests := []struct {
		name        string
		relType     parser.RelationshipType
		side        parser.RelationshipSide
		fkNullable  bool
		goType      string // deliberately mis-spelled to prove it is ignored
		wantGraphQL string
		wantIsList  bool
	}{
		{
			name:        "o2m ignores bare GoType",
			relType:     parser.OneToMany,
			side:        parser.SideParent,
			goType:      "Review", // old heuristic → "Review!" (non-null single)
			wantGraphQL: "[Review!]!",
			wantIsList:  true,
		},
		{
			name:        "m2m ignores pointer GoType",
			relType:     parser.ManyToMany,
			side:        parser.SideParent,
			goType:      "*Tag", // old heuristic → "Tag" (nullable single)
			wantGraphQL: "[Tag!]!",
			wantIsList:  true,
		},
		{
			name:        "o2o nullable regardless of bare GoType",
			relType:     parser.OneToOne,
			side:        parser.SideParent,
			goType:      "Company", // old heuristic → "Company!" (non-null)
			wantGraphQL: "Company",
			wantIsList:  false,
		},
		{
			name:        "o2o nullable even when FK column is non-null",
			relType:     parser.OneToOne,
			side:        parser.SideParent,
			fkNullable:  false, // FK column nullability must NOT force non-null
			goType:      "Company",
			wantGraphQL: "Company",
			wantIsList:  false,
		},
		{
			name:        "m2o inverse nullable regardless of non-pointer GoType",
			relType:     parser.OneToOne,
			side:        parser.SideChild,
			goType:      "Company", // old heuristic → "Company!" (non-null)
			wantGraphQL: "Company",
			wantIsList:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gen.RelationshipContext{
				Name:             "rel",
				Type:             tt.relType,
				Side:             tt.side,
				FKNullable:       tt.fkNullable,
				TargetStructName: targetStructName(tt.wantGraphQL),
				GoType:           tt.goType,
			}
			got := gen.MapRelationshipToGraphQLForTest(r, config.FieldCasingCamel)
			if got.GraphQLType != tt.wantGraphQL {
				t.Errorf("GraphQLType = %q, want %q", got.GraphQLType, tt.wantGraphQL)
			}
			if got.IsList != tt.wantIsList {
				t.Errorf("IsList = %v, want %v", got.IsList, tt.wantIsList)
			}
		})
	}
}

// targetStructName extracts the bare struct name from an expected GraphQL type
// so each test case only has to spell the target once.
func targetStructName(graphQLType string) string {
	r := strings.NewReplacer("[", "", "]", "", "!", "")
	return r.Replace(graphQLType)
}
