package mcp

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/teandresmith/sqlgen/manifest"
)

func TestFindReferencing(t *testing.T) {
	d := fixtureDeps()
	tests := []struct {
		name  string
		table string
		want  []Reference
	}{
		{
			name:  "users referenced by posts FK and both m2m junction sides",
			table: "users",
			want: []Reference{
				{Entity: "Post", RelationshipName: "posts", Kind: "o2m", FKColumn: "user_id"},
				// Both m2m relationships over user_roles reference users.
				{Entity: "Role", RelationshipName: "roles", Kind: "m2m", FKColumn: "user_id"},
				{Entity: "Role", RelationshipName: "users", Kind: "m2m", FKColumn: "user_id"},
			},
		},
		{
			name:  "posts referenced by comments FK from both relationship sides",
			table: "posts",
			want: []Reference{
				{Entity: "Comment", RelationshipName: "comments", Kind: "o2m", FKColumn: "post_id"},
				{Entity: "Comment", RelationshipName: "post", Kind: "m2o", FKColumn: "post_id"},
			},
		},
		{
			name:  "unknown table yields empty, not error",
			table: "nope",
			want:  []Reference{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := findReferencing(d, FindReferencingInput{Table: tt.table})
			if err != nil {
				t.Fatalf("findReferencing: %v", err)
			}
			if diff := cmp.Diff(tt.want, got.References); diff != "" {
				t.Errorf("references mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestFindReferencingKindTiebreak pins the sort order for two references that
// differ only in Kind — findReferencing dedups on the full Reference struct, so
// the sort key must be just as wide or the output order is undefined under the
// unstable sort.Slice (MCP.md §6.5). Target owns two same-named relationships to
// A over the same FK column (a.ref_id), differing only in kind; each reports a
// reference to Target's own table, so the two rows tie on every field but Kind.
func TestFindReferencingKindTiebreak(t *testing.T) {
	target := &manifest.Entity{
		Kind: manifest.EntityKindTable, Name: "Target", Table: "targets", PK: manifest.PK{Kind: "single"},
		Relationships: []manifest.Relationship{
			// FK lives on a (not targets) → referenced table is targets, reporting
			// entity is A, name "link", col ref_id — identical but for Kind.
			{Name: "link", Kind: manifest.RelationshipO2M, TargetEntity: "A", FK: &manifest.FK{Table: "a", Column: "ref_id"}},
			{Name: "link", Kind: manifest.RelationshipM2O, TargetEntity: "A", FK: &manifest.FK{Table: "a", Column: "ref_id"}},
		},
	}
	a := &manifest.Entity{
		Kind: manifest.EntityKindTable, Name: "A", Table: "a", PK: manifest.PK{Kind: "single"},
	}
	store := NewStore("/fixture/manifest_gen.json", nil)
	store.install(&manifest.Document{Layout: manifest.LayoutSingle}, nil, []*manifest.Entity{a, target})
	d := &toolDeps{store: store}

	first, err := findReferencing(d, FindReferencingInput{Table: "targets"})
	if err != nil {
		t.Fatalf("findReferencing: %v", err)
	}
	// m2o sorts before o2m lexicographically on the Kind tiebreak.
	want := []Reference{
		{Entity: "A", RelationshipName: "link", Kind: "m2o", FKColumn: "ref_id"},
		{Entity: "A", RelationshipName: "link", Kind: "o2m", FKColumn: "ref_id"},
	}
	if diff := cmp.Diff(want, first.References); diff != "" {
		t.Errorf("kind-tiebreak order mismatch (-want +got):\n%s", diff)
	}
	second, err := findReferencing(d, FindReferencingInput{Table: "targets"})
	if err != nil {
		t.Fatalf("findReferencing (repeat): %v", err)
	}
	if diff := cmp.Diff(first.References, second.References); diff != "" {
		t.Errorf("non-deterministic across runs (-first +second):\n%s", diff)
	}
}

// TestFindReferencingSameNamedTablesAcrossSchemas pins that the side holding
// the FK column is told apart by (schema, table), not the bare name: User
// (public.users) has-many AuditUser (audit.users) over audit.users.user_id. A
// bare compare reads the FK as living on public.users and reports User as the
// referencing entity. An FK with no Schema (a schema-less dialect) still
// matches on the bare name.
func TestFindReferencingSameNamedTablesAcrossSchemas(t *testing.T) {
	user := &manifest.Entity{
		Kind: manifest.EntityKindTable, Name: "User", Table: "users", Schema: "public", PK: manifest.PK{Kind: "single"},
		Relationships: []manifest.Relationship{
			{Name: "audit_users", Kind: manifest.RelationshipO2M, TargetEntity: "AuditUser", FK: &manifest.FK{Table: "users", Schema: "audit", Column: "user_id"}},
		},
	}
	auditUser := &manifest.Entity{
		Kind: manifest.EntityKindTable, Name: "AuditUser", Table: "users", Schema: "audit", PK: manifest.PK{Kind: "single"},
		Relationships: []manifest.Relationship{
			{Name: "user", Kind: manifest.RelationshipM2O, TargetEntity: "User", FK: &manifest.FK{Table: "users", Schema: "audit", Column: "user_id"}},
			// No FK.Schema: the bare-name fallback places the column on this table.
			{Name: "editor", Kind: manifest.RelationshipM2O, TargetEntity: "User", FK: &manifest.FK{Table: "users", Column: "editor_id"}},
		},
	}
	store := NewStore("/fixture/manifest_gen.json", nil)
	store.install(&manifest.Document{Layout: manifest.LayoutSingle}, nil, []*manifest.Entity{user, auditUser})
	d := &toolDeps{store: store}

	got, err := findReferencing(d, FindReferencingInput{Table: "users"})
	if err != nil {
		t.Fatalf("findReferencing: %v", err)
	}
	want := []Reference{
		{Entity: "AuditUser", RelationshipName: "audit_users", Kind: "o2m", FKColumn: "user_id"},
		{Entity: "AuditUser", RelationshipName: "editor", Kind: "m2o", FKColumn: "editor_id"},
		{Entity: "AuditUser", RelationshipName: "user", Kind: "m2o", FKColumn: "user_id"},
	}
	if diff := cmp.Diff(want, got.References); diff != "" {
		t.Errorf("references mismatch (-want +got):\n%s", diff)
	}
}

func TestDescribeRelationship(t *testing.T) {
	d := fixtureDeps()

	t.Run("m2m relationship with junction and target summary", func(t *testing.T) {
		got, err := describeRelationship(d, DescribeRelationshipInput{Entity: "User", Relationship: "roles"})
		if err != nil {
			t.Fatalf("describeRelationship: %v", err)
		}
		want := RelationshipDescription{
			Entity:       "User",
			Relationship: "roles",
			Kind:         "m2m",
			TargetEntity: "Role",
			Junction:     &manifest.Junction{Table: "user_roles", LocalFK: "user_id", TargetFK: "role_id"},
			Target:       &EntitySummary{Name: "Role", Table: "roles", PKKind: "single", FeaturesSummary: ""},
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("description mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("compact drops the target summary but keeps join shape", func(t *testing.T) {
		got, err := describeRelationship(d, DescribeRelationshipInput{Entity: "User", Relationship: "posts", Compact: true})
		if err != nil {
			t.Fatalf("describeRelationship: %v", err)
		}
		if got.Target != nil {
			t.Errorf("expected target summary dropped in compact mode, got %+v", got.Target)
		}
		if got.FK == nil || got.FK.Column != "user_id" {
			t.Errorf("expected FK join shape retained, got %+v", got.FK)
		}
	})

	t.Run("unknown entity yields -32003", func(t *testing.T) {
		_, err := describeRelationship(d, DescribeRelationshipInput{Entity: "Usr", Relationship: "roles"})
		assertNotFound(t, err, CodeEntityNotFound, []string{"User"})
	})

	t.Run("unknown relationship yields -32004 with suggestions", func(t *testing.T) {
		_, err := describeRelationship(d, DescribeRelationshipInput{Entity: "User", Relationship: "role"})
		assertNotFound(t, err, CodeRelationshipNotFound, []string{"roles"})
	})
}

func TestFindJoinPath(t *testing.T) {
	d := fixtureDeps()

	t.Run("one hop", func(t *testing.T) {
		got, err := findJoinPath(d, FindJoinPathInput{From: "User", To: "Role"})
		if err != nil {
			t.Fatalf("findJoinPath: %v", err)
		}
		want := []JoinPath{{Hops: 1, Path: []JoinHop{{Entity: "User", Relationship: "roles"}}}}
		if diff := cmp.Diff(want, got.Paths); diff != "" {
			t.Errorf("paths mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("two hops through Post", func(t *testing.T) {
		got, err := findJoinPath(d, FindJoinPathInput{From: "User", To: "Comment"})
		if err != nil {
			t.Fatalf("findJoinPath: %v", err)
		}
		want := []JoinPath{{Hops: 2, Path: []JoinHop{
			{Entity: "User", Relationship: "posts"},
			{Entity: "Post", Relationship: "comments"},
		}}}
		if diff := cmp.Diff(want, got.Paths); diff != "" {
			t.Errorf("paths mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("no path yields empty list", func(t *testing.T) {
		got, err := findJoinPath(d, FindJoinPathInput{From: "Membership", To: "User"})
		if err != nil {
			t.Fatalf("findJoinPath: %v", err)
		}
		if len(got.Paths) != 0 || got.Paths == nil {
			t.Errorf("expected empty non-nil paths, got %+v", got.Paths)
		}
	})

	t.Run("cycle guard: Comment back to itself has no path", func(t *testing.T) {
		got, err := findJoinPath(d, FindJoinPathInput{From: "Comment", To: "Comment"})
		if err != nil {
			t.Fatalf("findJoinPath: %v", err)
		}
		if len(got.Paths) != 0 {
			t.Errorf("cycle guard should prevent revisiting the start, got %+v", got.Paths)
		}
	})

	t.Run("max_hops boundary clamps the search", func(t *testing.T) {
		// User->Comment needs 2 hops; max_hops=1 finds nothing.
		one := 1
		got, err := findJoinPath(d, FindJoinPathInput{From: "User", To: "Comment", MaxHops: &one})
		if err != nil {
			t.Fatalf("findJoinPath: %v", err)
		}
		if len(got.Paths) != 0 {
			t.Errorf("max_hops=1 must exclude the 2-hop path, got %+v", got.Paths)
		}
		// max_hops=0 clamps up to 1 (still nothing); max_hops=6 is the upper clamp.
		zero := 0
		got, err = findJoinPath(d, FindJoinPathInput{From: "User", To: "Comment", MaxHops: &zero})
		if err != nil {
			t.Fatalf("findJoinPath: %v", err)
		}
		if len(got.Paths) != 0 {
			t.Errorf("max_hops=0 clamps to 1, expected nothing, got %+v", got.Paths)
		}
	})

	t.Run("unknown from/to yields -32003", func(t *testing.T) {
		_, err := findJoinPath(d, FindJoinPathInput{From: "Usr", To: "Role"})
		assertNotFound(t, err, CodeEntityNotFound, []string{"User"})

		_, err = findJoinPath(d, FindJoinPathInput{From: "User", To: "Rol"})
		assertNotFound(t, err, CodeEntityNotFound, []string{"Role"})
	})
}

// TestFindJoinPathOrderingAndCap builds a tiny fan-out graph to pin the
// shortest-first + lexicographic tie-break ordering and the ≤5 path cap.
func TestFindJoinPathOrderingAndCap(t *testing.T) {
	// A has six single-hop relationships to B, plus one two-hop route A->C->B.
	a := &manifest.Entity{
		Kind: manifest.EntityKindTable, Name: "A", Table: "a",
		PK: manifest.PK{Kind: "single"},
		Relationships: []manifest.Relationship{
			{Name: "r6", Kind: manifest.RelationshipO2M, TargetEntity: "B"},
			{Name: "r3", Kind: manifest.RelationshipO2M, TargetEntity: "B"},
			{Name: "r1", Kind: manifest.RelationshipO2M, TargetEntity: "B"},
			{Name: "r5", Kind: manifest.RelationshipO2M, TargetEntity: "B"},
			{Name: "r2", Kind: manifest.RelationshipO2M, TargetEntity: "B"},
			{Name: "r4", Kind: manifest.RelationshipO2M, TargetEntity: "B"},
			{Name: "viaC", Kind: manifest.RelationshipO2M, TargetEntity: "C"},
		},
	}
	c := &manifest.Entity{
		Kind: manifest.EntityKindTable, Name: "C", Table: "c", PK: manifest.PK{Kind: "single"},
		Relationships: []manifest.Relationship{{Name: "toB", Kind: manifest.RelationshipO2M, TargetEntity: "B"}},
	}
	b := &manifest.Entity{Kind: manifest.EntityKindTable, Name: "B", Table: "b", PK: manifest.PK{Kind: "single"}}

	store := NewStore("/fixture/manifest_gen.json", nil)
	store.install(&manifest.Document{Layout: manifest.LayoutSingle}, nil, []*manifest.Entity{a, b, c})
	d := &toolDeps{store: store}

	got, err := findJoinPath(d, FindJoinPathInput{From: "A", To: "B"})
	if err != nil {
		t.Fatalf("findJoinPath: %v", err)
	}
	// Seven routes exist (six 1-hop + one 2-hop); capped to 5, shortest-first,
	// then lexicographic on the relationship name.
	want := []JoinPath{
		{Hops: 1, Path: []JoinHop{{Entity: "A", Relationship: "r1"}}},
		{Hops: 1, Path: []JoinHop{{Entity: "A", Relationship: "r2"}}},
		{Hops: 1, Path: []JoinHop{{Entity: "A", Relationship: "r3"}}},
		{Hops: 1, Path: []JoinHop{{Entity: "A", Relationship: "r4"}}},
		{Hops: 1, Path: []JoinHop{{Entity: "A", Relationship: "r5"}}},
	}
	if diff := cmp.Diff(want, got.Paths); diff != "" {
		t.Errorf("ordering/cap mismatch (-want +got):\n%s", diff)
	}
}
