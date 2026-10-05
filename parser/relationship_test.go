package parser_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/parser"
)

func TestDetectRelationshipsM2MCompositePK(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "products",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
				},
			},
			{
				Name:   "tags",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
				},
			},
			{
				Name:   "product_tags",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "product_id", Type: "bigint", FKReference: &parser.FKReference{Table: "products", Schema: "public", Column: "id"}},
					{Name: "tag_id", Type: "bigint", FKReference: &parser.FKReference{Table: "tags", Schema: "public", Column: "id"}},
				},
				Constraints: []parser.Constraint{
					{
						Name:    "product_tags_pkey",
						Type:    parser.PrimaryKey,
						Columns: []string{"product_id", "tag_id"},
					},
				},
			},
		},
	}

	parser.DetectRelationships(schema)

	if len(schema.Relationships) != 2 {
		t.Fatalf("DetectRelationships() produced %d relationships, want 2", len(schema.Relationships))
	}

	want := []parser.Relationship{
		{
			Name:                "tags",
			BaseName:            "tags",
			Type:                parser.ManyToMany,
			Side:                parser.SideParent,
			SourceTable:         "public.products",
			TargetTable:         "public.tags",
			JunctionTable:       "public.product_tags",
			JunctionLocalFK:     "product_id",
			JunctionReferenceFK: "tag_id",
		},
		{
			Name:                "products",
			BaseName:            "products",
			Type:                parser.ManyToMany,
			Side:                parser.SideParent,
			SourceTable:         "public.tags",
			TargetTable:         "public.products",
			JunctionTable:       "public.product_tags",
			JunctionLocalFK:     "tag_id",
			JunctionReferenceFK: "product_id",
		},
	}

	if diff := cmp.Diff(want, schema.Relationships); diff != "" {
		t.Errorf("DetectRelationships() mismatch (-want +got):\n%s", diff)
	}
}

func TestDetectRelationshipsM2MCompositeUnique(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "students",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
				},
			},
			{
				Name:   "courses",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
				},
			},
			{
				Name:   "enrollments",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "student_id", Type: "bigint", FKReference: &parser.FKReference{Table: "students", Schema: "public", Column: "id"}},
					{Name: "course_id", Type: "bigint", FKReference: &parser.FKReference{Table: "courses", Schema: "public", Column: "id"}},
				},
				Constraints: []parser.Constraint{
					{
						Name:    "enrollments_student_course_uniq",
						Type:    parser.Unique,
						Columns: []string{"student_id", "course_id"},
					},
				},
			},
		},
	}

	parser.DetectRelationships(schema)

	if len(schema.Relationships) != 2 {
		t.Fatalf("DetectRelationships() produced %d relationships, want 2", len(schema.Relationships))
	}

	want := []parser.Relationship{
		{
			Name:                "courses",
			BaseName:            "courses",
			Type:                parser.ManyToMany,
			Side:                parser.SideParent,
			SourceTable:         "public.students",
			TargetTable:         "public.courses",
			JunctionTable:       "public.enrollments",
			JunctionLocalFK:     "student_id",
			JunctionReferenceFK: "course_id",
		},
		{
			Name:                "students",
			BaseName:            "students",
			Type:                parser.ManyToMany,
			Side:                parser.SideParent,
			SourceTable:         "public.courses",
			TargetTable:         "public.students",
			JunctionTable:       "public.enrollments",
			JunctionLocalFK:     "course_id",
			JunctionReferenceFK: "student_id",
		},
	}

	if diff := cmp.Diff(want, schema.Relationships); diff != "" {
		t.Errorf("DetectRelationships() mismatch (-want +got):\n%s", diff)
	}
}

func TestDetectRelationshipsO2O(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "users",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "profile_id", Type: "bigint", Unique: true, FKReference: &parser.FKReference{Table: "profiles", Schema: "public", Column: "id"}},
				},
			},
			{
				Name:   "profiles",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
				},
			},
		},
	}

	parser.DetectRelationships(schema)

	if len(schema.Relationships) != 1 {
		t.Fatalf("DetectRelationships() produced %d relationships, want 1", len(schema.Relationships))
	}

	want := parser.Relationship{
		Name:        "profiles",
		BaseName:    "profiles",
		Type:        parser.OneToOne,
		Side:        parser.SideParent,
		SourceTable: "public.users",
		TargetTable: "public.profiles",
		FKColumn:    "profile_id",
	}

	if diff := cmp.Diff(want, schema.Relationships[0]); diff != "" {
		t.Errorf("DetectRelationships() mismatch (-want +got):\n%s", diff)
	}
}

func TestDetectRelationshipsO2M(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "users",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
				},
			},
			{
				Name:   "orders",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "user_id", Type: "bigint", FKReference: &parser.FKReference{Table: "users", Schema: "public", Column: "id"}},
				},
			},
		},
	}

	parser.DetectRelationships(schema)

	if len(schema.Relationships) != 1 {
		t.Fatalf("DetectRelationships() produced %d relationships, want 1", len(schema.Relationships))
	}

	want := parser.Relationship{
		Name:        "orders",
		BaseName:    "orders",
		Type:        parser.OneToMany,
		Side:        parser.SideParent,
		SourceTable: "public.users",
		TargetTable: "public.orders",
		FKColumn:    "user_id",
	}

	if diff := cmp.Diff(want, schema.Relationships[0]); diff != "" {
		t.Errorf("DetectRelationships() mismatch (-want +got):\n%s", diff)
	}
}

// A 1:1 extension table declares its foreign key *as* the primary key, which is
// implicitly unique on every dialect — so it holds at most one row per parent
// and the edge is O2O. A columnUnique keyed only on Column.Unique and
// table-level UNIQUE constraints misses that, classifies the edge O2M and
// generates a list field for something structurally single-valued.
func TestDetectRelationshipsO2OViaSingleColumnPK(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "users",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
				},
			},
			{
				Name:   "user_credentials",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "user_id", Type: "uuid", PrimaryKey: true, FKReference: &parser.FKReference{Table: "users", Schema: "public", Column: "id"}},
					{Name: "provider", Type: "text"},
				},
			},
		},
	}

	parser.DetectRelationships(schema)

	if len(schema.Relationships) != 1 {
		t.Fatalf("DetectRelationships() produced %d relationships, want 1", len(schema.Relationships))
	}

	want := parser.Relationship{
		Name:        "users",
		BaseName:    "users",
		Type:        parser.OneToOne,
		Side:        parser.SideParent,
		SourceTable: "public.user_credentials",
		TargetTable: "public.users",
		FKColumn:    "user_id",
	}

	if diff := cmp.Diff(want, schema.Relationships[0]); diff != "" {
		t.Errorf("DetectRelationships() mismatch (-want +got):\n%s", diff)
	}
}

// The mirror of the case above: a member of a COMPOSITE primary key carries no
// uniqueness guarantee on its own, so its edges must stay O2M. This is the
// shape the single-column-PK clause must not over-reach into — a junction
// whose FK columns are both PK members.
func TestDetectRelationshipsCompositePKMemberIsNotUnique(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "users",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
				},
			},
			{
				Name:   "memberships",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "user_id", Type: "uuid", PrimaryKey: true, FKReference: &parser.FKReference{Table: "users", Schema: "public", Column: "id"}},
					{Name: "period", Type: "text", PrimaryKey: true},
				},
			},
		},
	}

	parser.DetectRelationships(schema)

	if len(schema.Relationships) != 1 {
		t.Fatalf("DetectRelationships() produced %d relationships, want 1", len(schema.Relationships))
	}
	if got := schema.Relationships[0].Type; got != parser.OneToMany {
		t.Errorf("DetectRelationships() type = %v, want %v", got, parser.OneToMany)
	}
}

func TestDetectRelationshipsSelfReferential(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "employees",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "manager_id", Type: "bigint", Nullable: true, FKReference: &parser.FKReference{Table: "employees", Schema: "public", Column: "id"}},
				},
			},
		},
	}

	parser.DetectRelationships(schema)

	if len(schema.Relationships) != 1 {
		t.Fatalf("DetectRelationships() produced %d relationships, want 1", len(schema.Relationships))
	}

	rel := schema.Relationships[0]
	if rel.Type != parser.OneToMany {
		t.Errorf("DetectRelationships() type = %v, want OneToMany", rel.Type)
	}
	if rel.SourceTable != "public.employees" {
		t.Errorf("DetectRelationships() SourceTable = %q, want %q", rel.SourceTable, "public.employees")
	}
	if rel.TargetTable != "public.employees" {
		t.Errorf("DetectRelationships() TargetTable = %q, want %q", rel.TargetTable, "public.employees")
	}
	if rel.FKColumn != "manager_id" {
		t.Errorf("DetectRelationships() FKColumn = %q, want %q", rel.FKColumn, "manager_id")
	}
}

func TestDetectRelationshipsThreePlusFKsInPKNotM2M(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{Name: "a", Columns: []parser.Column{{Name: "id", Type: "bigint", PrimaryKey: true}}},
			{Name: "b", Columns: []parser.Column{{Name: "id", Type: "bigint", PrimaryKey: true}}},
			{Name: "c", Columns: []parser.Column{{Name: "id", Type: "bigint", PrimaryKey: true}}},
			{
				Name: "abc_junction",
				Columns: []parser.Column{
					{Name: "a_id", Type: "bigint", FKReference: &parser.FKReference{Table: "a", Column: "id"}},
					{Name: "b_id", Type: "bigint", FKReference: &parser.FKReference{Table: "b", Column: "id"}},
					{Name: "c_id", Type: "bigint", FKReference: &parser.FKReference{Table: "c", Column: "id"}},
				},
				Constraints: []parser.Constraint{
					{
						Name:    "abc_junction_pkey",
						Type:    parser.PrimaryKey,
						Columns: []string{"a_id", "b_id", "c_id"},
					},
				},
			},
		},
	}

	parser.DetectRelationships(schema)

	// 3 FK columns → not M2M, should produce 3 O2M relationships instead.
	if len(schema.Relationships) != 3 {
		t.Fatalf("DetectRelationships() produced %d relationships, want 3", len(schema.Relationships))
	}

	for _, rel := range schema.Relationships {
		if rel.Type == parser.ManyToMany {
			t.Errorf("DetectRelationships() detected M2M for table with 3+ FKs in PK, want O2M")
		}
		if rel.Type != parser.OneToMany {
			t.Errorf("DetectRelationships() type = %v, want OneToMany", rel.Type)
		}
	}
}

func TestDetectRelationshipsCircularReferences(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "departments",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "head_employee_id", Type: "bigint", Unique: true, FKReference: &parser.FKReference{Table: "employees", Schema: "public", Column: "id"}},
				},
			},
			{
				Name:   "employees",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "department_id", Type: "bigint", FKReference: &parser.FKReference{Table: "departments", Schema: "public", Column: "id"}},
				},
			},
		},
	}

	parser.DetectRelationships(schema)

	if len(schema.Relationships) != 2 {
		t.Fatalf("DetectRelationships() produced %d relationships, want 2", len(schema.Relationships))
	}

	// departments.head_employee_id (UNIQUE) → O2O to employees.
	rel0 := schema.Relationships[0]
	if rel0.Type != parser.OneToOne {
		t.Errorf("Relationship[0] type = %v, want OneToOne", rel0.Type)
	}
	if rel0.SourceTable != "public.departments" || rel0.TargetTable != "public.employees" {
		t.Errorf("Relationship[0] = %q -> %q, want %q -> %q", rel0.SourceTable, rel0.TargetTable, "public.departments", "public.employees")
	}

	// employees.department_id (not unique) → O2M from departments to employees.
	rel1 := schema.Relationships[1]
	if rel1.Type != parser.OneToMany {
		t.Errorf("Relationship[1] type = %v, want OneToMany", rel1.Type)
	}
	if rel1.SourceTable != "public.departments" || rel1.TargetTable != "public.employees" {
		t.Errorf("Relationship[1] = %q -> %q, want %q -> %q", rel1.SourceTable, rel1.TargetTable, "public.departments", "public.employees")
	}
}

func TestDetectRelationshipsNoFKs(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "users",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "name", Type: "text"},
				},
			},
			{
				Name:   "products",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "title", Type: "text"},
				},
			},
		},
	}

	parser.DetectRelationships(schema)

	if len(schema.Relationships) != 0 {
		t.Errorf("DetectRelationships() produced %d relationships, want 0", len(schema.Relationships))
	}
}

// TestDetectRelationshipsO2MMultipleFKsToSameTarget pins that when a
// child table has multiple FK columns referencing the same parent, the
// inverse-side (parent→child) O2M relationships must get disambiguated names
// so the generated parent struct does not produce duplicate fields. Each
// emitted O2M edge carries a Name of "<fk-stem>_<child>" (role-first so the
// generated field reads "DeveloperAsset" — "the asset I am the developer
// of"). BaseName retains the original child table name for
// `exclude_relationships` bare-name matching. Single-FK edges to a
// different target keep current naming.
func TestDetectRelationshipsO2MMultipleFKsToSameTarget(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "stakeholder",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
				},
			},
			{
				Name:   "users",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
				},
			},
			{
				Name:   "asset",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "developer_id", Type: "uuid", FKReference: &parser.FKReference{Table: "stakeholder", Schema: "public", Column: "id"}},
					{Name: "om_provider_id", Type: "uuid", FKReference: &parser.FKReference{Table: "stakeholder", Schema: "public", Column: "id"}},
					{Name: "owner_id", Type: "uuid", FKReference: &parser.FKReference{Table: "stakeholder", Schema: "public", Column: "id"}},
					{Name: "created_by", Type: "uuid", FKReference: &parser.FKReference{Table: "users", Schema: "public", Column: "id"}},
				},
			},
		},
	}

	parser.DetectRelationships(schema)

	if got, want := len(schema.Relationships), 4; got != want {
		t.Fatalf("DetectRelationships() produced %d relationships, want %d", got, want)
	}

	// Collect Name by FKColumn for assertion clarity.
	byFK := make(map[string]parser.Relationship, len(schema.Relationships))
	for _, r := range schema.Relationships {
		if r.Type != parser.OneToMany {
			t.Errorf("relationship for FK %q has type %v, want OneToMany", r.FKColumn, r.Type)
		}
		byFK[r.FKColumn] = r
	}

	// Stakeholder → asset edges collide on Name="asset" → all three must
	// get a role-prefixed Name; BaseName stays as the child table for
	// downstream exclude-by-bare-name matching.
	cases := []struct {
		fkColumn     string
		wantName     string
		wantBaseName string
		wantSrc      string
	}{
		{"developer_id", "developer_asset", "asset", "public.stakeholder"},
		{"om_provider_id", "om_provider_asset", "asset", "public.stakeholder"},
		{"owner_id", "owner_asset", "asset", "public.stakeholder"},
		// users → asset is the only FK from asset to users → keeps the
		// undecorated current naming so single-FK schemas are unaffected.
		// Name and BaseName are identical in the single-edge case.
		{"created_by", "asset", "asset", "public.users"},
	}
	for _, c := range cases {
		got, ok := byFK[c.fkColumn]
		if !ok {
			t.Errorf("no relationship emitted for FK %q", c.fkColumn)
			continue
		}
		if got.Name != c.wantName {
			t.Errorf("FK %q: Name = %q, want %q", c.fkColumn, got.Name, c.wantName)
		}
		if got.BaseName != c.wantBaseName {
			t.Errorf("FK %q: BaseName = %q, want %q", c.fkColumn, got.BaseName, c.wantBaseName)
		}
		if got.SourceTable != c.wantSrc {
			t.Errorf("FK %q: SourceTable = %q, want %q", c.fkColumn, got.SourceTable, c.wantSrc)
		}
		if got.TargetTable != "public.asset" {
			t.Errorf("FK %q: TargetTable = %q, want %q", c.fkColumn, got.TargetTable, "public.asset")
		}
	}
}

// TestDetectRelationshipsO2OMultipleUniqueFKsToSameTarget pins the O2O variant
// of the name-disambiguation rule: a source table with multiple UNIQUE FK
// columns referencing the same target produces colliding Name="<target>"
// emissions without disambiguation. For O2O the disambiguator is the FK column
// with trailing "_id" stripped — replacing (not suffixing) the original target
// table name, since the Go target type is visible on the generated field
// declaration. BaseName preserves the target table name for
// exclude-by-bare-name matching.
func TestDetectRelationshipsO2OMultipleUniqueFKsToSameTarget(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "user_profile",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
				},
			},
			{
				Name:   "account",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "primary_profile_id", Type: "uuid", Unique: true, FKReference: &parser.FKReference{Table: "user_profile", Schema: "public", Column: "id"}},
					{Name: "billing_profile_id", Type: "uuid", Unique: true, FKReference: &parser.FKReference{Table: "user_profile", Schema: "public", Column: "id"}},
				},
			},
		},
	}

	parser.DetectRelationships(schema)

	if got, want := len(schema.Relationships), 2; got != want {
		t.Fatalf("DetectRelationships() produced %d relationships, want %d", got, want)
	}

	byFK := make(map[string]parser.Relationship, len(schema.Relationships))
	for _, r := range schema.Relationships {
		if r.Type != parser.OneToOne {
			t.Errorf("relationship for FK %q has type %v, want OneToOne", r.FKColumn, r.Type)
		}
		byFK[r.FKColumn] = r
	}

	cases := []struct {
		fkColumn     string
		wantName     string
		wantBaseName string
	}{
		{"primary_profile_id", "primary_profile", "user_profile"},
		{"billing_profile_id", "billing_profile", "user_profile"},
	}
	for _, c := range cases {
		got, ok := byFK[c.fkColumn]
		if !ok {
			t.Errorf("no relationship emitted for FK %q", c.fkColumn)
			continue
		}
		if got.Name != c.wantName {
			t.Errorf("FK %q: Name = %q, want %q", c.fkColumn, got.Name, c.wantName)
		}
		if got.BaseName != c.wantBaseName {
			t.Errorf("FK %q: BaseName = %q, want %q", c.fkColumn, got.BaseName, c.wantBaseName)
		}
	}
}

// TestDetectRelationshipsM2MMultipleJunctionsBetweenSamePair pins the M2M
// variant of the name-disambiguation rule: two junction tables joining the same
// pair of base tables collide on Name without disambiguation. Each colliding
// edge gets a suffix derived from the junction table base name.
func TestDetectRelationshipsM2MMultipleJunctionsBetweenSamePair(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "team",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
				},
			},
			{
				Name:   "users",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
				},
			},
			{
				Name:   "team_members",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "team_id", Type: "uuid", FKReference: &parser.FKReference{Table: "team", Schema: "public", Column: "id"}},
					{Name: "user_id", Type: "uuid", FKReference: &parser.FKReference{Table: "users", Schema: "public", Column: "id"}},
				},
				Constraints: []parser.Constraint{
					{Name: "team_members_pkey", Type: parser.PrimaryKey, Columns: []string{"team_id", "user_id"}},
				},
			},
			{
				Name:   "team_managers",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "team_id", Type: "uuid", FKReference: &parser.FKReference{Table: "team", Schema: "public", Column: "id"}},
					{Name: "user_id", Type: "uuid", FKReference: &parser.FKReference{Table: "users", Schema: "public", Column: "id"}},
				},
				Constraints: []parser.Constraint{
					{Name: "team_managers_pkey", Type: parser.PrimaryKey, Columns: []string{"team_id", "user_id"}},
				},
			},
		},
	}

	parser.DetectRelationships(schema)

	if got, want := len(schema.Relationships), 4; got != want {
		t.Fatalf("DetectRelationships() produced %d relationships, want %d", got, want)
	}

	type key struct{ src, tgt, junction string }
	byKey := make(map[key]parser.Relationship, len(schema.Relationships))
	for _, r := range schema.Relationships {
		if r.Type != parser.ManyToMany {
			t.Errorf("relationship %s→%s has type %v, want ManyToMany", r.SourceTable, r.TargetTable, r.Type)
		}
		byKey[key{r.SourceTable, r.TargetTable, r.JunctionTable}] = r
	}

	cases := []struct {
		k            key
		wantName     string
		wantBaseName string
	}{
		// Forward edges (team→users): BaseName is "users" (target table).
		// Disambiguated Name is the junction base name alone — replacing,
		// not suffixing, so the generated field reads "Team.TeamMembers".
		{key{"public.team", "public.users", "public.team_members"}, "team_members", "users"},
		{key{"public.team", "public.users", "public.team_managers"}, "team_managers", "users"},
		// Reverse edges (users→team): BaseName is "team".
		{key{"public.users", "public.team", "public.team_members"}, "team_members", "team"},
		{key{"public.users", "public.team", "public.team_managers"}, "team_managers", "team"},
	}
	for _, c := range cases {
		got, ok := byKey[c.k]
		if !ok {
			t.Errorf("no relationship emitted for %+v", c.k)
			continue
		}
		if got.Name != c.wantName {
			t.Errorf("%+v: Name = %q, want %q", c.k, got.Name, c.wantName)
		}
		if got.BaseName != c.wantBaseName {
			t.Errorf("%+v: BaseName = %q, want %q", c.k, got.BaseName, c.wantBaseName)
		}
	}
}

// TestDetectRelationshipsSelfReferentialMultipleFKs pins the
// name-disambiguation rule for the self-referential case: a table with two FK
// columns referencing itself collides on Name="<self>" without disambiguation.
// Verifies the post-pass applies uniformly when SourceTable == TargetTable.
func TestDetectRelationshipsSelfReferentialMultipleFKs(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "employees",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "manager_id", Type: "uuid", Nullable: true, FKReference: &parser.FKReference{Table: "employees", Schema: "public", Column: "id"}},
					{Name: "mentor_id", Type: "uuid", Nullable: true, FKReference: &parser.FKReference{Table: "employees", Schema: "public", Column: "id"}},
				},
			},
		},
	}

	parser.DetectRelationships(schema)

	if got, want := len(schema.Relationships), 2; got != want {
		t.Fatalf("DetectRelationships() produced %d relationships, want %d", got, want)
	}

	wantNames := map[string]string{
		"manager_id": "manager_employees",
		"mentor_id":  "mentor_employees",
	}
	for _, r := range schema.Relationships {
		want, ok := wantNames[r.FKColumn]
		if !ok {
			t.Errorf("unexpected FK column %q", r.FKColumn)
			continue
		}
		if r.Name != want {
			t.Errorf("FK %q: Name = %q, want %q", r.FKColumn, r.Name, want)
		}
		if r.BaseName != "employees" {
			t.Errorf("FK %q: BaseName = %q, want %q", r.FKColumn, r.BaseName, "employees")
		}
		if r.Type != parser.OneToMany {
			t.Errorf("FK %q: type = %v, want OneToMany", r.FKColumn, r.Type)
		}
	}
}

// TestDetectRelationshipsCrossKindCollisionToSameTarget pins the cross-kind
// extension of the name-disambiguation rule: when the same (source, target)
// pair carries both an M2M edge (via a junction table) AND an O2M edge (via a
// direct FK on the target), both originally emit Name="<target-table>" and
// collide on the generated parent struct field. The kind-agnostic grouping in
// disambiguateRelationshipNames detects the collision; each edge resolves to a
// unique name via its own per-type disambiguator (FK column for O2M, junction
// base name for M2M).
//
// Schema shape from the downstream consumer's bug report:
//   - organizations + users + organization_users (M2M junction)
//   - users.workspace_id → organizations (direct O2M)
//
// Both edges target SourceTable="organizations", TargetTable="users", so
// without the kind-agnostic key they each land in a singleton group and
// neither is disambiguated; the parent struct then emits two `Users []*User`
// fields. After the fix the M2M edge renders as "organization_users" and
// the O2M edge renders as "workspace_users" (or "workspace" alone if FK
// stripping reduces to nothing).
func TestDetectRelationshipsCrossKindCollisionToSameTarget(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "organizations",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
				},
			},
			{
				Name:   "users",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "uuid", PrimaryKey: true},
					{Name: "workspace_id", Type: "uuid", FKReference: &parser.FKReference{Table: "organizations", Schema: "public", Column: "id"}},
				},
			},
			{
				Name:   "organization_users",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "organization_id", Type: "uuid", FKReference: &parser.FKReference{Table: "organizations", Schema: "public", Column: "id"}},
					{Name: "user_id", Type: "uuid", FKReference: &parser.FKReference{Table: "users", Schema: "public", Column: "id"}},
				},
				Constraints: []parser.Constraint{
					{Name: "organization_users_pkey", Type: parser.PrimaryKey, Columns: []string{"organization_id", "user_id"}},
				},
			},
		},
	}

	parser.DetectRelationships(schema)

	// Three emissions: one O2M from users.workspace_id (orgs→users) + two
	// M2M directions from organization_users (orgs↔users).
	if got, want := len(schema.Relationships), 3; got != want {
		t.Fatalf("DetectRelationships() produced %d relationships, want %d", got, want)
	}

	// Look up the orgs→users edges: the O2M and the M2M forward direction
	// share (SourceTable, TargetTable) → cross-kind collision.
	var orgsToUsersO2M, orgsToUsersM2M *parser.Relationship
	for i := range schema.Relationships {
		r := &schema.Relationships[i]
		if r.SourceTable != "public.organizations" || r.TargetTable != "public.users" {
			continue
		}
		switch r.Type {
		case parser.OneToMany:
			orgsToUsersO2M = r
		case parser.ManyToMany:
			orgsToUsersM2M = r
		case parser.OneToOne:
			// not expected in this fixture
		}
	}
	if orgsToUsersO2M == nil {
		t.Fatal("no orgs→users OneToMany relationship emitted")
	}
	if orgsToUsersM2M == nil {
		t.Fatal("no orgs→users ManyToMany relationship emitted")
	}

	// O2M: FK column "workspace_id" → stem "workspace" → Name
	// "workspace_users". BaseName retains "users" for exclude-by-bare-name.
	if got, want := orgsToUsersO2M.Name, "workspace_users"; got != want {
		t.Errorf("O2M Name = %q, want %q", got, want)
	}
	if got, want := orgsToUsersO2M.BaseName, "users"; got != want {
		t.Errorf("O2M BaseName = %q, want %q", got, want)
	}

	// M2M: junction base name → Name "organization_users". BaseName
	// retains "users".
	if got, want := orgsToUsersM2M.Name, "organization_users"; got != want {
		t.Errorf("M2M Name = %q, want %q", got, want)
	}
	if got, want := orgsToUsersM2M.BaseName, "users"; got != want {
		t.Errorf("M2M BaseName = %q, want %q", got, want)
	}

	// And the cross-product cannot collide: O2M produces "workspace_users"
	// while M2M produces "organization_users" — distinct, valid Go
	// identifiers via flect.Pascalize.
	if orgsToUsersO2M.Name == orgsToUsersM2M.Name {
		t.Errorf("cross-kind disambiguation failed: both edges share Name=%q", orgsToUsersO2M.Name)
	}

	// The M2M reverse direction (users→organizations) is a singleton in
	// its (Source, Target) group and is left undecorated.
	var usersToOrgsM2M *parser.Relationship
	for i := range schema.Relationships {
		r := &schema.Relationships[i]
		if r.SourceTable == "public.users" && r.TargetTable == "public.organizations" && r.Type == parser.ManyToMany {
			usersToOrgsM2M = r
			break
		}
	}
	if usersToOrgsM2M == nil {
		t.Fatal("no users→organizations ManyToMany relationship emitted")
	}
	if got, want := usersToOrgsM2M.Name, "organizations"; got != want {
		t.Errorf("singleton M2M reverse Name = %q, want %q (single-edge group, no disambiguation)", got, want)
	}
}

// TestDetectRelationshipsPartialUniqueDoesNotPromoteToO2O pins the
// partial-UNIQUE gate: a partial UNIQUE constraint on an FK column (Where !=
// "") must not promote an O2M edge to O2O. A predicate-gated uniqueness only
// holds for rows matching the predicate, so the FK can still produce duplicate
// child rows for the same parent on the excluded side of the predicate.
func TestDetectRelationshipsPartialUniqueDoesNotPromoteToO2O(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{
				Name:   "users",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
				},
			},
			{
				Name:   "sessions",
				Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "user_id", Type: "bigint", FKReference: &parser.FKReference{Table: "users", Schema: "public", Column: "id"}},
				},
				Constraints: []parser.Constraint{
					{
						Name:    "sessions_active_user_uq",
						Type:    parser.Unique,
						Columns: []string{"user_id"},
						Method:  "btree",
						Where:   "revoked_at IS NULL",
					},
				},
			},
		},
	}

	parser.DetectRelationships(schema)

	if len(schema.Relationships) != 1 {
		t.Fatalf("DetectRelationships() produced %d relationships, want 1", len(schema.Relationships))
	}
	if got, want := schema.Relationships[0].Type, parser.OneToMany; got != want {
		t.Errorf("partial UNIQUE on FK promoted edge to %v, want %v (stay O2M)", got, want)
	}

	// Sanity: dropping the Where would flip the edge to O2O — proves the gate.
	schema2 := &parser.Schema{
		Tables: []parser.Table{
			{
				Name: "users", Schema: "public",
				Columns: []parser.Column{{Name: "id", Type: "bigint", PrimaryKey: true}},
			},
			{
				Name: "sessions", Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "user_id", Type: "bigint", FKReference: &parser.FKReference{Table: "users", Schema: "public", Column: "id"}},
				},
				Constraints: []parser.Constraint{
					{Name: "sessions_user_uq", Type: parser.Unique, Columns: []string{"user_id"}},
				},
			},
		},
	}
	parser.DetectRelationships(schema2)
	if len(schema2.Relationships) != 1 || schema2.Relationships[0].Type != parser.OneToOne {
		t.Fatalf("control: dropping Where should produce a single O2O; got %+v", schema2.Relationships)
	}
}

// TestDetectRelationshipsPartialUniqueDoesNotAnchorM2M pins the partial-UNIQUE
// gate on the junction-detection path: a composite partial UNIQUE (covering
// both FK columns) must not anchor an M2M junction. Without the gate,
// junctionConstraint would mis-classify the table as M2M and emit bidirectional
// relationships.
func TestDetectRelationshipsPartialUniqueDoesNotAnchorM2M(t *testing.T) {
	schema := &parser.Schema{
		Tables: []parser.Table{
			{Name: "users", Schema: "public", Columns: []parser.Column{{Name: "id", Type: "bigint", PrimaryKey: true}}},
			{Name: "teams", Schema: "public", Columns: []parser.Column{{Name: "id", Type: "bigint", PrimaryKey: true}}},
			{
				Name: "team_invites", Schema: "public",
				Columns: []parser.Column{
					{Name: "id", Type: "bigint", PrimaryKey: true},
					{Name: "user_id", Type: "bigint", FKReference: &parser.FKReference{Table: "users", Schema: "public", Column: "id"}},
					{Name: "team_id", Type: "bigint", FKReference: &parser.FKReference{Table: "teams", Schema: "public", Column: "id"}},
				},
				Constraints: []parser.Constraint{
					{
						Name:    "team_invites_pending_uq",
						Type:    parser.Unique,
						Columns: []string{"user_id", "team_id"},
						Method:  "btree",
						Where:   "status = 'pending'",
					},
				},
			},
		},
	}

	parser.DetectRelationships(schema)

	for _, r := range schema.Relationships {
		if r.Type == parser.ManyToMany {
			t.Errorf("partial composite UNIQUE anchored an M2M edge; relationships=%+v", schema.Relationships)
			break
		}
	}
}
