package gen_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
	"github.com/teandresmith/sqlgen/cmd/sqlgen/gen"
	"github.com/teandresmith/sqlgen/parser"
)

// keyMembersDDL holds one edge per pinning case, under tenancy on
// workspace_id:
//
//   - Cycle.Tasks: workspace_id is the tenant column, cycle_id is free.
//   - Cycle.PinnedTasks: workspace_id is the tenant column and cycle_id is
//     also held by a single-column FK, so no member is free.
//   - Zone.Sites: region_id is also held by sites.region_id → regions, so
//     zone_id is the one free member.
//   - Zone.Notes: neither member is pinned.
//   - Zone.FullSites and Zone.FullNotes: Sites and Notes under MATCH FULL.
//   - User.Profile: the one member, user_id, is the child's primary key.
//
// PinnedTasks and FullSites list their nullable free member first, so an edge
// read through its first member alone looks rewritable.
const keyMembersDDL = `
CREATE TABLE workspaces (id bigint PRIMARY KEY, name text NOT NULL);
CREATE TABLE cycles (
	id bigint PRIMARY KEY,
	workspace_id bigint NOT NULL REFERENCES workspaces(id),
	name text NOT NULL,
	UNIQUE (workspace_id, id)
);
CREATE TABLE tasks (
	id bigint PRIMARY KEY,
	workspace_id bigint NOT NULL,
	cycle_id bigint,
	title text NOT NULL,
	FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles (workspace_id, id)
);
CREATE TABLE pinned_tasks (
	id bigint PRIMARY KEY,
	workspace_id bigint NOT NULL REFERENCES workspaces(id),
	cycle_id bigint REFERENCES cycles(id),
	title text NOT NULL,
	FOREIGN KEY (cycle_id, workspace_id) REFERENCES cycles (id, workspace_id)
);
CREATE TABLE regions (id bigint PRIMARY KEY, name text NOT NULL);
CREATE TABLE zones (
	id bigint PRIMARY KEY,
	region_id bigint NOT NULL REFERENCES regions(id),
	name text NOT NULL,
	UNIQUE (region_id, id)
);
CREATE TABLE sites (
	id bigint PRIMARY KEY,
	region_id bigint NOT NULL REFERENCES regions(id),
	zone_id bigint,
	name text NOT NULL,
	FOREIGN KEY (region_id, zone_id) REFERENCES zones (region_id, id)
);
CREATE TABLE notes (
	id bigint PRIMARY KEY,
	zone_region bigint,
	zone_id bigint,
	body text NOT NULL,
	FOREIGN KEY (zone_region, zone_id) REFERENCES zones (region_id, id)
);
CREATE TABLE full_sites (
	id bigint PRIMARY KEY,
	region_id bigint NOT NULL REFERENCES regions(id),
	zone_id bigint,
	name text NOT NULL,
	FOREIGN KEY (zone_id, region_id) REFERENCES zones (id, region_id) MATCH FULL
);
CREATE TABLE full_notes (
	id bigint PRIMARY KEY,
	zone_region bigint,
	zone_id bigint,
	body text NOT NULL,
	FOREIGN KEY (zone_region, zone_id) REFERENCES zones (region_id, id) MATCH FULL
);
CREATE TABLE users (id bigint PRIMARY KEY, name text NOT NULL);
CREATE TABLE profiles (user_id bigint PRIMARY KEY REFERENCES users(id), bio text);
`

// keyMembersContexts builds ddl with nested mutations on and tenancy on
// workspace_id, with edges through composite keys generated. mutate adjusts
// the config first.
func keyMembersContexts(t *testing.T, dialect config.Dialect, ddl string, mutate func(*config.RootConfig)) map[string]gen.TableContext {
	t.Helper()
	contexts, err := buildKeyMembersContexts(t, dialect, ddl, mutate)
	if err != nil {
		t.Fatalf("BuildTableContextsFromSchema() error: %v", err)
	}
	return contexts
}

// buildKeyMembersContexts is keyMembersContexts returning the build error.
func buildKeyMembersContexts(t *testing.T, dialect config.Dialect, ddl string, mutate func(*config.RootConfig)) (map[string]gen.TableContext, error) {
	t.Helper()
	gen.GenerateCompositeKeyEdgesForTest(t)
	schema := parseDDLForTest(t, dialect, ddl)
	parser.DetectRelationships(schema)
	cfg := testInput(schema).Config
	cfg.Input.Dialect = dialect
	cfg.Generation.NestedMutations = &config.NestedMutationsConfig{Enabled: new(true)}
	cfg.Tenancy = &config.TenancyConfig{Enabled: true, Column: "workspace_id", Required: new(true)}
	if mutate != nil {
		mutate(cfg)
	}
	contexts, err := gen.BuildTableContextsFromSchema(schema, cfg)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]gen.TableContext, len(contexts))
	for _, tc := range contexts {
		byName[tc.TableName] = tc
	}
	return byName, nil
}

// verbs is the rewrite half of a nested edge's verb set.
type verbs struct{ create, connect, disconnect, clear bool }

func edgeVerbs(e gen.NestedEdgeContext) verbs {
	return verbs{e.HasCreate, e.HasConnect, e.HasDisconnect, e.HasClear}
}

// TestNestedEligibility_PinnedAndFreeMembers pins PRD §9.9.2: a key member is
// pinned when it is the tenant column, a member of the child's primary key or
// a member of another FOREIGN KEY on the child. The rewrite verbs need a free
// member, and an edge with none keeps `create` only.
func TestNestedEligibility_PinnedAndFreeMembers(t *testing.T) {
	// Detection gives profiles the belongs-to side only; the has-one side is
	// declared.
	contexts := keyMembersContexts(t, config.DialectPostgres, keyMembersDDL, func(cfg *config.RootConfig) {
		cfg.Tables["users"] = config.TableConfig{Relationships: []config.TableRelationship{
			{Name: "Profile", Type: "o2o", Table: "profiles", FK: config.KeyColumns{"user_id"}},
		}}
	})
	all := verbs{create: true, connect: true, disconnect: true, clear: true}
	tests := []struct {
		name         string
		parent, edge string
		pinned, free []string
		want         verbs
	}{
		{name: "tenant column", parent: "cycles", edge: "Tasks", pinned: []string{"workspace_id"}, free: []string{"cycle_id"}, want: all},
		{name: "child primary-key member", parent: "users", edge: "Profile", pinned: []string{"user_id"}, want: verbs{create: true}},
		{name: "member shared with another key", parent: "zones", edge: "Sites", pinned: []string{"region_id"}, free: []string{"zone_id"}, want: all},
		{name: "every member pinned keeps create only", parent: "cycles", edge: "PinnedTasks", pinned: []string{"cycle_id", "workspace_id"}, want: verbs{create: true}},
		{name: "free member", parent: "zones", edge: "Notes", free: []string{"zone_region", "zone_id"}, want: all},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := nestedEdge(t, contexts[tt.parent], tt.edge)
			if !slices.Equal(e.PinnedMembers, tt.pinned) || !slices.Equal(e.FreeMembers, tt.free) {
				t.Errorf("%s.%s pinned, free = %q, %q; want %q, %q", tt.parent, tt.edge, e.PinnedMembers, e.FreeMembers, tt.pinned, tt.free)
			}
			if got := edgeVerbs(e); got != tt.want {
				t.Errorf("%s.%s verbs = %+v, want %+v", tt.parent, tt.edge, got, tt.want)
			}
			// The parent's referenced columns set every member, so the child
			// input drops them all (PRD §9.9.5).
			for _, f := range e.ChildFields {
				if slices.Contains(tt.pinned, f.ColumnName) || slices.Contains(tt.free, f.ColumnName) {
					t.Errorf("%s.%s child input %s keeps key member %s", tt.parent, tt.edge, e.ChildInputName, f.ColumnName)
				}
			}
		})
	}
}

// TestNestedEligibility_MatchFull pins PRD §9.9.4 (E8) for PostgreSQL MATCH
// FULL: a key may not mix NULL and non-NULL members, so `disconnect` and
// `clear` need every member free and nullable, and a key with a pinned member
// is never unparented, so `connect` needs `allow_reparent`. MySQL and SQLite
// enforce MATCH SIMPLE whatever the DDL says.
func TestNestedEligibility_MatchFull(t *testing.T) {
	all := verbs{create: true, connect: true, disconnect: true, clear: true}
	reparent := func(cfg *config.RootConfig) {
		cfg.Tables["zones"] = config.TableConfig{NestedMutations: &config.TableNestedMutationsConfig{
			Relationships: []config.TableNestedRelationship{
				{Name: "FullSites", AllowReparent: true}, {Name: "FullNotes"}, {Name: "Sites"}, {Name: "Notes"},
			},
		}}
	}
	postgres := []config.Dialect{config.DialectPostgres}
	tests := []struct {
		name     string
		dialects []config.Dialect
		mutate   func(*config.RootConfig)
		edge     string
		want     verbs
	}{
		{name: "simple with nullable free members", dialects: postgres, edge: "Sites", want: all},
		{name: "full with a pinned member has no disconnect", dialects: postgres, edge: "FullSites", want: verbs{create: true}},
		{name: "full with every member free keeps disconnect", dialects: postgres, edge: "FullNotes", want: all},
		{
			name: "full with a pinned member connects only under allow_reparent", dialects: postgres,
			mutate: reparent, edge: "FullSites", want: verbs{create: true, connect: true},
		},
		{
			name:     "mysql and sqlite read full as simple",
			dialects: []config.Dialect{config.DialectMySQL, config.DialectSQLite}, edge: "FullSites", want: all,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, dialect := range tt.dialects {
				ddl := keyMembersDDL
				if dialect == config.DialectSQLite {
					ddl = strings.ReplaceAll(ddl, "bigint PRIMARY KEY", "integer PRIMARY KEY")
				}
				e := nestedEdge(t, keyMembersContexts(t, dialect, ddl, tt.mutate)["zones"], tt.edge)
				if got := edgeVerbs(e); got != tt.want {
					t.Errorf("%s: zones.%s verbs = %+v, want %+v", dialect, tt.edge, got, tt.want)
				}
			}
		})
	}
}

// TestNestedEligibility_M2MIntoAUniqueKeyIsNotWriteEligible pins E5's binding
// clause (PRD §9.9.4): the link step writes the target's primary key into the
// junction's reference FK, so a junction keyed on one of the target's UNIQUE
// columns cannot be written from it, even when the two share a type. The edge
// is omitted, and reported when listed.
func TestNestedEligibility_M2MIntoAUniqueKeyIsNotWriteEligible(t *testing.T) {
	const ddl = `
CREATE TABLE users (id bigint PRIMARY KEY, name text NOT NULL);
CREATE TABLE tags (id bigint PRIMARY KEY, legacy_id bigint NOT NULL UNIQUE);
CREATE TABLE user_tags (
	user_id bigint NOT NULL REFERENCES users(id),
	tag_legacy_id bigint NOT NULL REFERENCES tags(legacy_id),
	PRIMARY KEY (user_id, tag_legacy_id)
);`
	contexts := keyMembersContexts(t, config.DialectPostgres, ddl, nil)
	if slices.Contains(nestedEdgeNames(contexts["users"]), "Tags") {
		t.Errorf("auto-included: users nested edges = %v, want Tags not write-eligible", nestedEdgeNames(contexts["users"]))
	}
	_, err := buildKeyMembersContexts(t, config.DialectPostgres, ddl, func(cfg *config.RootConfig) {
		cfg.Tables["users"] = config.TableConfig{NestedMutations: &config.TableNestedMutationsConfig{
			Relationships: []config.TableNestedRelationship{{Name: "Tags"}},
		}}
	})
	const want = `the junction's reference FK "tag_legacy_id" references the target's "legacy_id" rather than its primary key`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("listed: error = %v, want it to contain %q", err, want)
	}
}
