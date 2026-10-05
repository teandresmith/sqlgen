package manifest_test

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/teandresmith/sqlgen/manifest"
)

// fullEntity returns a representative full Entity exercising every nested type
// (columns with extended metadata, indexes, features, relationships, methods
// with multi-dialect sql_bodies, filter/sort, examples).
func fullEntity() manifest.Entity {
	return manifest.Entity{
		Kind:       manifest.EntityKindTable,
		Name:       "User",
		Table:      "users",
		Schema:     "public",
		FilePrefix: "users",
		Files:      []string{"users_gen.go"},
		Comment:    "application <users>",
		Indexes: []manifest.Index{
			{Name: "users_pkey", Columns: []string{"id"}, Unique: true, Method: "btree"},
			{Name: "users_email_key", Columns: []string{"email"}, Unique: true, Method: "btree", Where: "deleted_at IS NULL"},
		},
		PK: manifest.PK{
			Kind:    "single",
			Columns: []manifest.PKColumn{{Name: "id", GoField: "ID", GoType: "uuid.UUID"}},
		},
		Features: manifest.Features{
			SoftDelete:   &manifest.SoftDeleteFeature{Column: "deleted_at", Type: "timestamp"},
			Cache:        &manifest.CacheFeature{TTLSeconds: 60, Hydration: "lazy", KeyPattern: "user:{id}", InvalidatesOn: []string{"Update", "Delete"}},
			Events:       &manifest.EventsFeature{Enabled: true, Types: []string{"created", "updated"}, PayloadShape: "User"},
			Tenancy:      &manifest.TenancyFeature{Column: "tenant_id", Mode: "strict", MissingResolverError: "ErrTenantMissing", MismatchError: "ErrTenantMismatch"},
			AuditColumns: []string{"created_at", "updated_at"},
		},
		Columns: []manifest.Column{
			{Name: "id", GoField: "ID", GoType: "uuid.UUID", DBType: "uuid", PK: true, Default: "gen_random_uuid()", DefaultKind: "function", Comment: "primary key"},
			{Name: "email", GoField: "Email", GoType: "string", DBType: "text", Unique: true, Comment: "login email", Check: "email <> ''"},
			{Name: "status", GoField: "Status", GoType: "string", DBType: "text", Nullable: true, Default: "'pending'", DefaultKind: "literal", Comparator: "comparator.String", Comment: ""},
		},
		Relationships: []manifest.Relationship{
			{Name: "Orders", Kind: manifest.RelationshipO2M, TargetEntity: "Order", FK: &manifest.FK{Table: "orders", Column: "user_id"}},
			{Name: "Roles", Kind: manifest.RelationshipM2M, TargetEntity: "Role", Junction: &manifest.Junction{Table: "user_roles", LocalFK: "user_id", TargetFK: "role_id"}},
		},
		Methods: manifest.Methods{
			Query: []manifest.Method{
				{
					Name:    "FindByID",
					Params:  []manifest.MethodParam{{Name: "id", Type: "uuid.UUID"}},
					Returns: "*User",
					Errors:  []string{"ErrNotFound"},
					SQLBodies: map[manifest.Dialect]string{
						manifest.DialectPostgres: `SELECT * FROM "users" WHERE "id" = $1`,
						manifest.DialectMySQL:    "SELECT * FROM `users` WHERE `id` = ?",
					},
				},
				{
					Name:   "FindByEmail",
					Params: []manifest.MethodParam{{Name: "email", Type: "string"}},
					Source: &manifest.MethodSource{Kind: "unique_index", Columns: []string{"email"}},
				},
			},
			Mutation: []manifest.Method{
				{Name: "Create", Params: []manifest.MethodParam{{Name: "input", Type: "CreateUserInput"}}, Returns: "*User"},
			},
		},
		Filter: manifest.Filter{
			Type:   "UserFilter",
			Fields: []manifest.FilterField{{Name: "Email", Type: "comparator.String"}},
		},
		Sort: manifest.Sort{Type: "UserSort", Fields: []string{"CreatedAt"}},
		Examples: &manifest.Examples{
			Read:  []string{"c.Q.User().FindByID(ctx, id)"},
			Write: []string{"c.M.User().Create(ctx, input)"},
		},
	}
}

func fullDocument() manifest.Document {
	return manifest.Document{
		SchemaVersion: "0.1.0",
		GeneratedAt:   "2026-07-09T00:00:00Z",
		Generator:     manifest.Generator{Name: "sqlgen", Version: "v1.2.3"},
		Dialect:       manifest.DialectPostgres,
		Package:       "store",
		Layout:        manifest.LayoutSingle,
		Conventions: manifest.Conventions{
			ClientEntryPoints:       manifest.ClientEntryPoints{Query: "c.Q", Mutation: "c.M"},
			ErrorSentinels:          []manifest.ErrorSentinel{{Name: "ErrNotFound", GraphQLCode: "NOT_FOUND", Package: "store"}},
			FindReturnsNilOnMissing: true,
			Pagination:              manifest.PaginationConvention{PageType: "Page", ListEnvelopeSuffix: "List", CursorEncoding: "base64"},
			CallOptions:             manifest.CallOptionsConvention{Type: "CallOption", Fields: []string{"Tx"}},
			SoftDelete:              manifest.SoftDeleteConvention{DefaultExcludedFromFinds: true, IncludeVia: "IncludeDeleted", HardDeleteMethodSuffix: "HardDelete", RestoreMethodSuffix: "Restore"},
			Comparator:              manifest.ComparatorConvention{Package: "comparator", Families: []string{"String", "Number"}, ShapeNotes: []string{"eq/neq"}, Composition: map[string]string{"and": "And"}, Examples: map[string]string{"eq": "comparator.String{Eq: ..}"}},
			Omittable:               manifest.OmittableConvention{Package: "omittable", Type: "Value[T]", Purpose: "distinguish unset", Construction: []string{"omittable.Set(x)"}, Methods: []string{"IsSet"}, JSONBehavior: "omit when unset"},
		},
		GenerationConfig: manifest.GenerationConfig{Cache: true, SoftDelete: true},
		Entities: []manifest.EntityIndex{
			{Name: "User", Table: "users", Kind: manifest.EntityKindTable},
			{Name: "ActiveUser", Table: "active_users", Kind: manifest.EntityKindView},
		},
		Enums:  []manifest.Enum{{Name: "Status", GoType: "Status", DBType: "status", Values: []string{"pending", "active"}}},
		Extras: []manifest.Extra{{Kind: "composite", Name: "Address", GoType: "Address", Fields: []manifest.ExtraField{{Name: "City", GoType: "string", JSON: "city"}}}},
	}
}

// TestDocument_RoundTrip marshals a fully-populated Document and unmarshals it
// back, asserting deep equality — pins the JSON tags across every named type.
func TestDocument_RoundTrip(t *testing.T) {
	want := fullDocument()

	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got manifest.Document
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Document round-trip mismatch (-want +got):\n%s", diff)
	}
}

// TestEntity_RoundTrip marshals a fully-populated Entity and unmarshals it back,
// covering the full per-entity record shape (the payload of each per_entity
// file and each inline single-layout entity).
func TestEntity_RoundTrip(t *testing.T) {
	want := fullEntity()

	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got manifest.Entity
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Entity round-trip mismatch (-want +got):\n%s", diff)
	}
}

// TestGenerationConfig_Decode confirms each of the eight toggles decodes
// correctly in both true and false states (8 toggles × 2 states = 16 cases).
func TestGenerationConfig_Decode(t *testing.T) {
	cases := []struct {
		key string
		get func(manifest.GenerationConfig) bool
	}{
		{"audit_columns", func(g manifest.GenerationConfig) bool { return g.AuditColumns }},
		{"cache", func(g manifest.GenerationConfig) bool { return g.Cache }},
		{"events", func(g manifest.GenerationConfig) bool { return g.Events }},
		{"graphql", func(g manifest.GenerationConfig) bool { return g.GraphQL }},
		{"graph_top_level", func(g manifest.GenerationConfig) bool { return g.GraphTopLevel }},
		{"soft_delete", func(g manifest.GenerationConfig) bool { return g.SoftDelete }},
		{"tenancy", func(g manifest.GenerationConfig) bool { return g.Tenancy }},
		{"views", func(g manifest.GenerationConfig) bool { return g.Views }},
	}

	if len(cases) != 8 {
		t.Fatalf("expected 8 toggles, got %d", len(cases))
	}

	for _, tc := range cases {
		for _, state := range []bool{true, false} {
			name := tc.key
			if state {
				name += "_true"
			} else {
				name += "_false"
			}
			t.Run(name, func(t *testing.T) {
				var g manifest.GenerationConfig
				body := []byte(`{"` + tc.key + `":` + boolLit(state) + `}`)
				if err := json.Unmarshal(body, &g); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if got := tc.get(g); got != state {
					t.Errorf("%s decoded to %v, want %v", tc.key, got, state)
				}
			})
		}
	}
}

// TestMethod_SQLBodiesDecode confirms the per-dialect sql_bodies map decodes
// with a typed Dialect key.
func TestMethod_SQLBodiesDecode(t *testing.T) {
	body := []byte(`{
		"name": "List",
		"params": [],
		"returns": "[]*User",
		"errors": [],
		"sql_bodies": {
			"postgres": "SELECT * FROM \"users\" WHERE <filter> ORDER BY <sort> LIMIT $1",
			"mysql": "SELECT * FROM ` + "`users`" + ` WHERE <filter> ORDER BY <sort> LIMIT ?"
		}
	}`)

	var m manifest.Method
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	want := map[manifest.Dialect]string{
		manifest.DialectPostgres: `SELECT * FROM "users" WHERE <filter> ORDER BY <sort> LIMIT $1`,
		manifest.DialectMySQL:    "SELECT * FROM `users` WHERE <filter> ORDER BY <sort> LIMIT ?",
	}
	if diff := cmp.Diff(want, m.SQLBodies); diff != "" {
		t.Errorf("SQLBodies mismatch (-want +got):\n%s", diff)
	}
}

func boolLit(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
