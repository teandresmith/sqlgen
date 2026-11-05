package gen_test

import (
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// pinnedFKDDL holds the two single-column FKs whose one member is pinned:
// profiles.user_id is the child's primary key, and projects.workspace_id is
// the tenant column.
const pinnedFKDDL = `
CREATE TABLE users (
	id uuid PRIMARY KEY,
	name text NOT NULL
);
CREATE TABLE profiles (
	user_id uuid PRIMARY KEY REFERENCES users(id),
	bio text NOT NULL
);
CREATE TABLE workspaces (
	id uuid PRIMARY KEY,
	name text NOT NULL
);
CREATE TABLE projects (
	id uuid PRIMARY KEY,
	workspace_id uuid NOT NULL REFERENCES workspaces(id),
	name text NOT NULL
);
`

// pinnedFKContexts builds pinnedFKDDL with nested mutations on and tenancy on
// workspace_id.
func pinnedFKContexts(t *testing.T) map[string]gen.TableContext {
	t.Helper()
	schema := parseDDLForTest(t, config.DialectPostgres, pinnedFKDDL)
	parser.DetectRelationships(schema)
	cfg := testInput(schema).Config
	cfg.Generation.NestedMutations = &config.NestedMutationsConfig{Enabled: new(true)}
	cfg.Tenancy = &config.TenancyConfig{Enabled: true, Column: "workspace_id", Required: new(true)}
	// Detection gives profiles the belongs-to side only; the has-one side is
	// declared.
	cfg.Tables["users"] = config.TableConfig{Relationships: []config.TableRelationship{
		{Name: "Profile", Type: "o2o", Table: "profiles", FK: config.KeyColumns{"user_id"}},
	}}

	contexts, err := gen.BuildTableContextsFromSchema(schema, cfg)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema() error: %v", err)
	}
	byName := make(map[string]gen.TableContext, len(contexts))
	for _, tc := range contexts {
		byName[tc.TableName] = tc
	}
	return byName
}

// TestNestedEligibility_SingleColumnPinnedFKIsCreateOnly pins PRD §9.9.2 for a
// single-column FK: a member that is the child's PK or the tenant column is
// NOT NULL, so the edge keeps `create` and has no `connect`, `disconnect` or
// `clear`.
func TestNestedEligibility_SingleColumnPinnedFKIsCreateOnly(t *testing.T) {
	contexts := pinnedFKContexts(t)
	tests := []struct {
		name   string
		parent string
		edge   string
	}{
		{name: "child primary-key member", parent: "users", edge: "Profile"},
		{name: "tenant column", parent: "workspaces", edge: "Projects"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := nestedEdge(t, contexts[tt.parent], tt.edge)
			if !e.HasCreate {
				t.Error("HasCreate = false, want true")
			}
			if e.HasConnect || e.HasDisconnect || e.HasClear {
				t.Errorf("connect, disconnect, clear = %v, %v, %v; want create only", e.HasConnect, e.HasDisconnect, e.HasClear)
			}
		})
	}
}

// TestNestedEligibility_ChildInputDropsTenantFKColumn pins PRD §9.9.5 for a
// single-column FK on the tenant column: the nested child input drops it, as
// it drops any traversed FK, and the parent's key sets it.
func TestNestedEligibility_ChildInputDropsTenantFKColumn(t *testing.T) {
	e := nestedEdge(t, pinnedFKContexts(t)["workspaces"], "Projects")
	for _, f := range e.ChildFields {
		if f.ColumnName == "workspace_id" {
			t.Fatalf("child input %s keeps workspace_id, the traversed tenant FK", e.ChildInputName)
		}
	}
	if e.FKFieldName != "WorkspaceID" {
		t.Errorf("FKFieldName = %q, want WorkspaceID", e.FKFieldName)
	}
}
