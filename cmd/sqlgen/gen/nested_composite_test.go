package gen_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/config"
)

// compositeNestedDDL holds one composite edge per shape, under tenancy on
// workspace_id: Cycle.Tasks (O2M), Team.Labels (M2M over a tenant junction) and
// Zone.Sectors (a key of two free members).
const compositeNestedDDL = `
CREATE TABLE cycles (id bigint PRIMARY KEY, workspace_id bigint NOT NULL, UNIQUE (workspace_id, id));
CREATE TABLE tasks (
	id bigint PRIMARY KEY,
	workspace_id bigint NOT NULL,
	cycle_id bigint,
	title text NOT NULL,
	FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles (workspace_id, id)
);
CREATE TABLE teams (id bigint PRIMARY KEY, workspace_id bigint NOT NULL, UNIQUE (workspace_id, id));
CREATE TABLE labels (id bigint PRIMARY KEY, workspace_id bigint NOT NULL, UNIQUE (workspace_id, id));
CREATE TABLE team_labels (
	workspace_id bigint NOT NULL,
	team_id bigint NOT NULL,
	label_id bigint NOT NULL,
	PRIMARY KEY (workspace_id, team_id, label_id),
	FOREIGN KEY (workspace_id, team_id) REFERENCES teams (workspace_id, id),
	FOREIGN KEY (workspace_id, label_id) REFERENCES labels (workspace_id, id)
);
CREATE TABLE zones (id bigint PRIMARY KEY, region bigint NOT NULL, UNIQUE (region, id));
CREATE TABLE sectors (
	id bigint PRIMARY KEY,
	zone_region bigint,
	zone_id bigint,
	FOREIGN KEY (zone_region, zone_id) REFERENCES zones (region, id)
);`

// flat collapses every run of whitespace, so a test can match generated code
// across the template's line breaks and before gofumpt aligns it.
func flat(s string) string { return strings.Join(strings.Fields(s), " ") }

func assertRendered(t *testing.T, got string, wants, absent []string) {
	t.Helper()
	got = flat(got)
	for _, want := range wants {
		if !strings.Contains(got, flat(want)) {
			t.Errorf("rendered executor is missing %q", want)
		}
	}
	for _, not := range absent {
		if strings.Contains(got, flat(not)) {
			t.Errorf("rendered executor contains %q", not)
		}
	}
}

// executorOf returns the rendered executor of one edge.
func executorOf(t *testing.T, out, name string) string {
	t.Helper()
	marker := ") " + name + "("
	at := strings.Index(out, marker)
	if at < 0 {
		t.Fatalf("no executor %s in the rendered surface", name)
	}
	end := strings.Index(out[at:], "\n}\n")
	if end < 0 {
		return out[at:]
	}
	return out[at : at+end]
}

// TestNestedTemplate_CompositeKeyExecutor pins the statements of a key with two
// columns at N = 2 (PRD §9.9.3, §9.9.6): `fk = parent` compares every member,
// SET writes the free members only, and `connect` verifies the pinned ones.
func TestNestedTemplate_CompositeKeyExecutor(t *testing.T) {
	contexts := keyMembersContexts(t, config.DialectPostgres, compositeNestedDDL, nil)
	reparent := keyMembersContexts(t, config.DialectPostgres, compositeNestedDDL, func(cfg *config.RootConfig) {
		cfg.Tables["cycles"] = config.TableConfig{NestedMutations: &config.TableNestedMutationsConfig{
			Relationships: []config.TableNestedRelationship{{Name: "Tasks", AllowReparent: true}},
		}}
	})
	cycles := executorOf(t, renderNested(t, contexts["cycles"]), "applyCycleTasksNested")
	reparenting := executorOf(t, renderNested(t, reparent["cycles"]), "applyCycleTasksNested")
	zones := executorOf(t, renderNested(t, contexts["zones"]), "applyZoneSectorsNested")
	teams := executorOf(t, renderNested(t, contexts["teams"]), "applyTeamLabelsNested")

	const pinnedScope = `WorkspaceID: &comparator.Number[int64]{Eq: new(pid0)}, CycleID: &comparator.NullableNumber[int64]{Number: comparator.Number[int64]{Eq: new(pid1)}},`

	tests := []struct {
		name          string
		got           string
		wants, absent []string
	}{
		{
			name: "create sets every member from the parent",
			got:  cycles,
			wants: []string{
				"WorkspaceID: omittable.Set(parent.WorkspaceID), CycleID: omittable.Set(&parent.ID),",
			},
		},
		{
			name: "a tenancy-set value that disagrees is ErrAlreadyRelated",
			got:  cycles,
			wants: []string{
				`if errors.Is(err, tenancy.ErrMismatch) { err = ErrAlreadyRelated }`,
			},
		},
		{
			name: "the visibility read selects every member",
			got:  cycles,
			wants: []string{
				"FieldOptions{ ID: true, WorkspaceID: true, CycleID: true}",
			},
		},
		{
			name: "connect verifies the pinned member before any write",
			got:  cycles,
			wants: []string{
				"owner := make(map[int64][2]string, len(connectIDs))",
				"want := [2]string{fmt.Sprint(parent.WorkspaceID), fmt.Sprint(parent.ID)}",
				`case cur[0] != want[0]:`,
				`case !null[1] && cur == want:`,
				`case !null[1]: return nestedError("Tasks", "connect", v, ErrAlreadyRelated)`,
			},
		},
		{
			name:  "connect writes the free member alone",
			got:   cycles,
			wants: []string{"&UpdateTaskInput{ CycleID: omittable.Set(&parent.ID) }"},
			absent: []string{
				"&UpdateTaskInput{ WorkspaceID",
			},
		},
		{
			name:   "allow_reparent drops the guard and keeps the pinned check",
			got:    reparenting,
			wants:  []string{`case cur[0] != want[0]:`},
			absent: []string{"Null: new(true)", "case !null[1]:"},
		},
		{
			name:  "the adoption's verify read compares the whole key",
			got:   cycles,
			wants: []string{"ID: &comparator.Number[int64]{In: adopting}, " + pinnedScope},
		},
		{
			name:  "disconnect nulls the free member and scopes on the whole key",
			got:   cycles,
			wants: []string{"ID: &comparator.Number[int64]{In: ids}, " + pinnedScope, "&UpdateTaskInput{ CycleID: omittable.Set[*int64](nil) }"},
		},
		{
			name: "two free members are guarded and written together",
			got:  zones,
			wants: []string{
				"ZoneRegion: &comparator.NullableNumber[int64]{Null: new(true)}, ZoneID: &comparator.NullableNumber[int64]{Null: new(true)},",
				"&UpdateSectorInput{ ZoneRegion: omittable.Set(&parent.Region), ZoneID: omittable.Set(&parent.ID) }",
				"&UpdateSectorInput{ ZoneRegion: omittable.Set[*int64](nil), ZoneID: omittable.Set[*int64](nil) }",
				"case !null[0] || !null[1]:",
			},
		},
		{
			name: "m2m link writes the union of both keys",
			got:  teams,
			wants: []string{
				"&CreateTeamLabelInput{ WorkspaceID: parent.WorkspaceID, TeamID: parent.ID, LabelID: tid, }",
			},
		},
		{
			name: "m2m disconnect deletes by the union",
			got:  teams,
			wants: []string{
				"TeamLabelPK{ WorkspaceID: parent.WorkspaceID, TeamID: parent.ID, LabelID: tid, }",
			},
		},
		{
			name: "m2m clear filters on the whole local key",
			got:  teams,
			wants: []string{
				"&TeamLabelFilter{ WorkspaceID: &comparator.Number[int64]{Eq: new(pid0)}, TeamID: &comparator.Number[int64]{Eq: new(pid1)}, }",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRendered(t, tt.got, tt.wants, tt.absent)
		})
	}
}

// The parent write selects every column an edge's key references, whatever the
// caller selected, so a narrowed selection cannot zero what the executors copy
// (PRD §9.6).
func TestNestedTemplate_ParentWriteForcesKeyColumns(t *testing.T) {
	tests := []struct{ name, parent string }{{"o2m", "cycles"}, {"m2m", "teams"}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderNested(t, keyMembersContexts(t, config.DialectPostgres, compositeNestedDDL, nil)[tt.parent])
			assertRendered(t, got, []string{"scalars.ID = true scalars.WorkspaceID = true return &scalars"}, nil)
		})
	}
	t.Run("a key into the primary key forces nothing more", func(t *testing.T) {
		const ddl = `CREATE TABLE users (id bigint PRIMARY KEY, workspace_id bigint NOT NULL);
CREATE TABLE orders (id bigint PRIMARY KEY, user_id bigint REFERENCES users(id));`
		got := renderNested(t, keyMembersContexts(t, config.DialectPostgres, ddl, nil)["users"])
		assertRendered(t, got, []string{"scalars.ID = true return &scalars"}, nil)
	})
}

// A has-one edge through the tenant column maps a mismatch as an O2M does.
func TestNestedTemplate_HasOneTenantKeyMapsTheMismatch(t *testing.T) {
	const ddl = `CREATE TABLE workspaces (id bigint PRIMARY KEY);
CREATE TABLE briefs (id bigint PRIMARY KEY, workspace_id bigint NOT NULL UNIQUE REFERENCES workspaces(id));`
	contexts := keyMembersContexts(t, config.DialectPostgres, ddl, func(cfg *config.RootConfig) {
		cfg.Tables["workspaces"] = config.TableConfig{Relationships: []config.TableRelationship{
			{Name: "Brief", Type: "o2o", Table: "briefs", FK: config.KeyColumns{"workspace_id"}},
		}}
	})
	got := executorOf(t, renderNested(t, contexts["workspaces"]), "applyWorkspaceBriefNested")
	assertRendered(t, got, []string{"if errors.Is(err, tenancy.ErrMismatch) { err = ErrAlreadyRelated }"}, nil)
}

// TestNestedTemplate_ChildCreateInputDropsEveryKeyMember pins PRD §9.9.5: the
// parent's referenced columns set every member, so the child input declares
// none of them, the tenant column included.
func TestNestedTemplate_ChildCreateInputDropsEveryKeyMember(t *testing.T) {
	tests := []struct {
		name, ddl, parent, input, keeps string
		drops                           []string
	}{
		{
			name: "composite key", ddl: compositeNestedDDL, parent: "cycles",
			input: "type CycleTasksCreateInput struct {", keeps: "Title string", drops: []string{"WorkspaceID", "CycleID"},
		},
		{
			name: "single-column tenant key", parent: "workspaces",
			ddl: `CREATE TABLE workspaces (id bigint PRIMARY KEY);
CREATE TABLE projects (id bigint PRIMARY KEY, workspace_id bigint NOT NULL REFERENCES workspaces(id), name text NOT NULL);`,
			input: "type WorkspaceProjectsCreateInput struct {", keeps: "Name string", drops: []string{"WorkspaceID"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := renderNested(t, keyMembersContexts(t, config.DialectPostgres, tt.ddl, nil)[tt.parent])
			start := strings.Index(out, tt.input)
			if start < 0 {
				t.Fatalf("no %q in the rendered surface", tt.input)
			}
			body := out[start : start+strings.Index(out[start:], "\n}\n")]
			if !strings.Contains(flat(body), tt.keeps) {
				t.Errorf("child input lost %q:\n%s", tt.keeps, body)
			}
			for _, drop := range tt.drops {
				if strings.Contains(body, drop) {
					t.Errorf("child input keeps key member %s:\n%s", drop, body)
				}
			}
		})
	}
}

// TestNestedEligibility_ReferencedNullableMemberIsNotWriteEligible pins E1 over
// every referenced member (PRD §9.9.4): the second column of the referenced key
// is the nullable one, so reading the first alone passes.
func TestNestedEligibility_ReferencedNullableMemberIsNotWriteEligible(t *testing.T) {
	const ddl = `
CREATE TABLE shelves (id bigint PRIMARY KEY, region text NOT NULL, bin text, UNIQUE (region, bin));
CREATE TABLE crates (id bigint PRIMARY KEY, shelf_region text, shelf_bin text,
  FOREIGN KEY (shelf_region, shelf_bin) REFERENCES shelves (region, bin));`
	if got := nestedEdgeNames(keyMembersContexts(t, config.DialectPostgres, ddl, nil)["shelves"]); len(got) != 0 {
		t.Errorf("shelves nested edges = %q, want none", got)
	}
	_, err := buildKeyMembersContexts(t, config.DialectPostgres, ddl, func(cfg *config.RootConfig) {
		cfg.Tables["shelves"] = config.TableConfig{NestedMutations: &config.TableNestedMutationsConfig{
			Relationships: []config.TableNestedRelationship{{Name: "Crates"}},
		}}
	})
	const want = `the edge's key references the nullable column "bin"`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("listed: error = %v, want it to contain %q", err, want)
	}
}

// TestNestedEligibility_TenantJunctionUnlinkNeedsPKOverTheUnion pins E5 and the
// unlink half of PRD §9.9.3 over a tenant junction, whose two keys share a
// column: the link step writes their union, and `disconnect` needs a junction
// primary key equal to it.
func TestNestedEligibility_TenantJunctionUnlinkNeedsPKOverTheUnion(t *testing.T) {
	const parents = `
CREATE TABLE teams (id bigint PRIMARY KEY, workspace_id bigint NOT NULL, UNIQUE (workspace_id, id));
CREATE TABLE labels (id bigint PRIMARY KEY, workspace_id bigint NOT NULL, UNIQUE (workspace_id, id));`
	tests := []struct {
		name, junction, target string
		want                   verbs
	}{
		{
			name:   "pk equals union",
			target: "TeamLabelConflictPK",
			junction: `CREATE TABLE team_labels (workspace_id bigint NOT NULL, team_id bigint NOT NULL, label_id bigint NOT NULL,
  PRIMARY KEY (workspace_id, team_id, label_id),
  FOREIGN KEY (workspace_id, team_id) REFERENCES teams (workspace_id, id),
  FOREIGN KEY (workspace_id, label_id) REFERENCES labels (workspace_id, id));`,
			want: verbs{create: true, connect: true, disconnect: true, clear: true},
		},
		{
			name:   "surrogate pk has no disconnect",
			target: "TeamLabelConflictWorkspaceIDTeamIDLabelID",
			junction: `CREATE TABLE team_labels (id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, workspace_id bigint NOT NULL,
  team_id bigint NOT NULL, label_id bigint NOT NULL,
  UNIQUE (workspace_id, team_id, label_id),
  FOREIGN KEY (workspace_id, team_id) REFERENCES teams (workspace_id, id),
  FOREIGN KEY (workspace_id, label_id) REFERENCES labels (workspace_id, id));`,
			want: verbs{create: true, connect: true, clear: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			teams := keyMembersContexts(t, config.DialectPostgres, parents+tt.junction, nil)["teams"]
			e := nestedEdge(t, teams, "Labels")
			if got := edgeVerbs(e); got != tt.want {
				t.Errorf("teams.Labels verbs = %+v, want %+v", got, tt.want)
			}
			if e.JunctionConflictTarget != tt.target {
				t.Errorf("teams.Labels link target = %q, want %q", e.JunctionConflictTarget, tt.target)
			}
		})
	}
}

// The link step targets the conflict target equal to the union of both junction
// keys, and an edge with neither a PRIMARY KEY nor a UNIQUE equal to it is
// refused: a listed edge fails generation, an auto-included one is omitted.
func TestNestedEligibility_JunctionLinkTargetIsTheUnionOfItsKeys(t *testing.T) {
	const parents = `
CREATE TABLE teams (id bigint PRIMARY KEY, name text NOT NULL);
CREATE TABLE labels (id bigint PRIMARY KEY, name text NOT NULL);`
	const fks = `team_id bigint NOT NULL REFERENCES teams (id), label_id bigint NOT NULL REFERENCES labels (id)`
	const surrogate = `CREATE TABLE team_labels (id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, ` + fks
	tests := []struct {
		name, junction string
		// target is the constant the link step passes, "" when the edge is refused.
		target string
	}{
		{name: "unique over the union", junction: surrogate + `, UNIQUE (team_id, label_id));`, target: "TeamLabelConflictTeamIDLabelID"},
		{name: "column order does not matter", junction: surrogate + `, UNIQUE (label_id, team_id));`, target: "TeamLabelConflictLabelIDTeamID"},
		{name: "a unique index counts", junction: surrogate + `); CREATE UNIQUE INDEX team_labels_uq ON team_labels (team_id, label_id);`, target: "TeamLabelConflictTeamIDLabelID"},
		{name: "no unique", junction: surrogate + `);`},
		{name: "partial unique", junction: surrogate + `, deleted_at timestamptz); CREATE UNIQUE INDEX team_labels_uq ON team_labels (team_id, label_id) WHERE deleted_at IS NULL;`},
		{name: "unique narrower than the union", junction: surrogate + `, UNIQUE (team_id));`},
		{name: "unique wider than the union", junction: surrogate + `, UNIQUE (id, team_id, label_id));`},
		{name: "unique of the union's size over other columns", junction: surrogate + `, UNIQUE (id, team_id));`},
		{name: "pk narrower than the union", junction: `CREATE TABLE team_labels (team_id bigint PRIMARY KEY REFERENCES teams (id), label_id bigint NOT NULL REFERENCES labels (id));`},
	}
	declared := func(listed bool) func(*config.RootConfig) {
		return func(cfg *config.RootConfig) {
			tc := config.TableConfig{Relationships: []config.TableRelationship{{
				Name: "Labels", Type: "many_to_many", Table: "labels", Junction: "team_labels",
				JunctionLocalFK: config.KeyColumns{"team_id"}, JunctionReferenceFK: config.KeyColumns{"label_id"},
			}}}
			if listed {
				tc.NestedMutations = &config.TableNestedMutationsConfig{Relationships: []config.TableNestedRelationship{{Name: "Labels"}}}
			}
			cfg.Tables["teams"] = tc
		}
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ddl := parents + tt.junction
			_, listedErr := buildKeyMembersContexts(t, config.DialectPostgres, ddl, declared(true))
			teams := keyMembersContexts(t, config.DialectPostgres, ddl, declared(false))["teams"]
			if tt.target == "" {
				const want = "conflict-target constant equal to the union of its two keys"
				if listedErr == nil || !strings.Contains(listedErr.Error(), want) {
					t.Errorf("listed: error = %v, want it to contain %q", listedErr, want)
				}
				if slices.Contains(nestedEdgeNames(teams), "Labels") {
					t.Error("auto-included edge survived with no conflict target equal to the union")
				}
				return
			}
			if listedErr != nil {
				t.Fatalf("listed: error = %v, want nil", listedErr)
			}
			e := nestedEdge(t, teams, "Labels")
			if e.JunctionConflictTarget != tt.target {
				t.Errorf("link target = %q, want %q", e.JunctionConflictTarget, tt.target)
			}
			if want := (verbs{create: true, connect: true, clear: true}); edgeVerbs(e) != want {
				t.Errorf("verbs = %+v, want %+v", edgeVerbs(e), want)
			}
		})
	}
}

// A junction column no parent column supplies comes off the target, so the link
// step reads it, and `disconnect`, which reads nothing, cannot name the row.
func TestNestedEligibility_JunctionReferencingTargetColumn(t *testing.T) {
	const ddl = `
CREATE TABLE users (id bigint PRIMARY KEY, name text NOT NULL);
CREATE TABLE tags (id bigint PRIMARY KEY, legacy_id bigint NOT NULL UNIQUE);
CREATE TABLE user_tags (
	user_id bigint NOT NULL REFERENCES users(id),
	tag_legacy_id bigint NOT NULL REFERENCES tags(legacy_id),
	PRIMARY KEY (user_id, tag_legacy_id)
);`
	e := nestedEdge(t, keyMembersContexts(t, config.DialectPostgres, ddl, nil)["users"], "Tags")
	if got, want := edgeVerbs(e), (verbs{create: true, connect: true, clear: true}); got != want {
		t.Errorf("users.Tags verbs = %+v, want %+v", got, want)
	}
	_, err := buildKeyMembersContexts(t, config.DialectPostgres, ddl, func(cfg *config.RootConfig) {
		cfg.Tables["users"] = config.TableConfig{NestedMutations: &config.TableNestedMutationsConfig{
			Relationships: []config.TableNestedRelationship{{Name: "Tags"}},
		}}
	})
	const want = "cannot carry its `disconnect` verb"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("listed: error = %v, want it to contain %q", err, want)
	}

	out := flat(renderNested(t, keyMembersContexts(t, config.DialectPostgres, ddl, nil)["users"]))
	for _, want := range []string{
		"TagFieldOptions{ ID: true, LegacyID: true}",
		"targetRows[row.ID] = row",
		"row := targetRows[tid]",
		"TagLegacyID: row.LegacyID,",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered surface is missing %q", want)
		}
	}
}
