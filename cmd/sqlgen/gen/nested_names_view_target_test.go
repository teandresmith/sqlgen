package gen_test

import (
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// TestValidateResolvedNames_EdgeIntoViewClaimsNestedNames pins the nested
// claim for an edge whose target is not a generated table: PRD §8.5 claims the
// names of an O2O edge whose target is outside the generated set, where
// has-one and belongs-to cannot be told apart, as it does for an O2M edge.
func TestValidateResolvedNames_EdgeIntoViewClaimsNestedNames(t *testing.T) {
	tests := []struct {
		name    string
		relType string
		relName string
		wantRel string
		want    string
	}{
		{name: "one-to-many into a view", relType: "one_to_many", relName: "Profiles", wantRel: "relationship users.Profiles", want: `"UserProfilesCreateNested"`},
		{name: "one-to-one into a view", relType: "one_to_one", relName: "Profile", wantRel: "relationship users.Profile", want: `"UserProfileCreateNested"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &parser.Schema{
				Tables: []parser.Table{
					namedTable("users", ""),
					namedTable("user_profile_create_nesteds", ""),
					namedTable("user_profiles_create_nesteds", ""),
				},
				Views: []parser.View{{
					Name: "profile_views",
					Columns: []parser.Column{
						{Name: "id", Type: "uuid"},
						{Name: "user_id", Type: "uuid"},
					},
				}},
			}
			cfg := nameRuleConfig()
			cfg.Tables = map[string]config.TableConfig{
				"users": {Relationships: []config.TableRelationship{{
					Name: tt.relName, Type: tt.relType, Table: "profile_views", FK: config.KeyColumns{"user_id"},
				}}},
			}

			_, err := gen.ValidateGeneration(schema, cfg)
			if err == nil {
				t.Fatalf("ValidateGeneration() error = nil, want the collision on %s", tt.want)
			}
			for _, want := range []string{tt.wantRel, tt.want} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("ValidateGeneration() error = %v, want it to name %s", err, want)
				}
			}
		})
	}
}
