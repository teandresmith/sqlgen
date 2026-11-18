package gen_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// TestAPIDeleteVariants_CompositePKSoftDeleteTable pins PRD §26.5.1's delete
// naming for a composite-PK table with a soft-delete column: both variants on
// give hardDelete / softDelete / restore, each taking one argument per key
// column, and no bare delete.
func TestAPIDeleteVariants_CompositePKSoftDeleteTable(t *testing.T) {
	res := trsGenerate(t, config.DialectPostgres,
		`CREATE TABLE memberships (org_id uuid NOT NULL, user_id uuid NOT NULL, role text NOT NULL, deleted_at timestamptz, PRIMARY KEY (org_id, user_id));`,
		pprGraphQLConfig("", trsSoftDeleteTop))
	schema := pprReadGenerated(t, res, "models/graph/membership_gen.graphqls")
	resolvers := pprReadGenerated(t, res, "models/graph/sqlgenresolver/resolvers_gen.go")

	tests := []struct {
		name       string
		field      string
		method     string
		clientCall string
	}{
		{name: "hard delete takes every key column and returns Boolean", field: "hardDeleteMembership(orgID: UUID!, userID: UUID!): Boolean!", method: "HardDeleteMembership", clientCall: ".HardDelete("},
		{name: "soft delete takes every key column and returns the nullable row", field: "softDeleteMembership(orgID: UUID!, userID: UUID!): Membership", method: "SoftDeleteMembership", clientCall: ".SoftDelete("},
		{name: "restore takes every key column and returns the nullable row", field: "restoreMembership(orgID: UUID!, userID: UUID!): Membership", method: "RestoreMembership", clientCall: ".Restore("},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !pprHasLine(schema, tt.field) {
				t.Errorf("schema has no line %q:\n%s", tt.field, schema)
			}
			if body := extractFunc(t, resolvers, tt.method); !strings.Contains(body, tt.clientCall) {
				t.Errorf("%s does not call %s:\n%s", tt.method, tt.clientCall, body)
			}
		})
	}
	t.Run("no bare delete", func(t *testing.T) {
		if slices.Contains(trsRootFields(schema, "Mutation"), "deleteMembership") {
			t.Errorf("both variants on, yet a bare deleteMembership is declared:\n%s", schema)
		}
	})
}
