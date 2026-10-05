package mcp

import (
	"time"

	"github.com/teandresmith/sqlgen/manifest"
)

// fixtureLoadedAt is a fixed load timestamp so health output is deterministic in
// tests (the real server sets this to time.Now at load).
var fixtureLoadedAt = time.Date(2026, 5, 15, 14, 22, 11, 0, time.UTC)

// newFixtureStore builds a Store populated with the synthetic manifest below,
// without touching disk. It covers the tool fixture shapes: single-PK
// (User/Post/Role/Comment), composite-PK + tenanted (Membership), view
// (ActiveUser), m2m (User.roles / Role.users), and soft-deleted (User).
func newFixtureStore() *Store {
	s := NewStore("/fixture/manifest/manifest_gen.json", nil)
	s.install(fixtureDoc(), fixtureRaw, fixtureEntities())
	s.loadedAt = fixtureLoadedAt
	s.ok = true
	return s
}

// fixtureDeps wraps newFixtureStore in the tool dependency struct with watch on.
func fixtureDeps() *toolDeps {
	return &toolDeps{store: newFixtureStore(), version: "9.9.9", watchEnabled: true}
}

// fixtureRaw is a minimal, schema-agnostic manifest body backing validate/raw
// paths that only need the schema_version probe. Schema-validating tests load a
// real fixture file instead.
var fixtureRaw = []byte(`{"schema_version":"0.1.0"}`)

func fixtureDoc() *manifest.Document {
	return &manifest.Document{
		SchemaVersion: "0.1.0",
		GeneratedAt:   "2026-05-15T14:22:11Z",
		Generator:     manifest.Generator{Name: "sqlgen", Version: "0.42.0"},
		Dialect:       manifest.DialectPostgres,
		Package:       "models",
		Layout:        manifest.LayoutSingle,
		Conventions: manifest.Conventions{
			ClientEntryPoints:       manifest.ClientEntryPoints{Query: "client.<Entity>()", Mutation: "client.<Entity>()"},
			ErrorSentinels:          []manifest.ErrorSentinel{{Name: "ErrNotFound", GraphQLCode: "NOT_FOUND", Package: "models"}},
			FindReturnsNilOnMissing: true,
			Pagination:              manifest.PaginationConvention{PageType: "Page", ListEnvelopeSuffix: "List", CursorEncoding: "base64"},
			// The real shape (PRD §9.6). Spelled out rather than left as filler
			// because `Tx` is a field the generated CallOptions has never had, and
			// a fixture carrying it is how the builder's own list came to claim it.
			CallOptions: manifest.CallOptionsConvention{Type: "CallOptions", Fields: []string{"SkipCache", "SkipEvents", "SkipHooks", "SkipTenancy", "Tenant", "FieldOptions", "LockMode", "AllowInTransaction"}},
		},
		GenerationConfig: manifest.GenerationConfig{SoftDelete: true, Tenancy: true, Views: true},
		Entities: []manifest.EntityIndex{
			{Name: "ActiveUser", Table: "active_users", Kind: manifest.EntityKindView},
			{Name: "Comment", Table: "comments", Kind: manifest.EntityKindTable},
			{Name: "Membership", Table: "memberships", Kind: manifest.EntityKindTable},
			{Name: "Post", Table: "posts", Kind: manifest.EntityKindTable},
			{Name: "Role", Table: "roles", Kind: manifest.EntityKindTable},
			{Name: "User", Table: "users", Kind: manifest.EntityKindTable},
		},
	}
}

func fixtureEntities() []*manifest.Entity {
	pg := manifest.DialectPostgres
	user := &manifest.Entity{
		Kind: manifest.EntityKindTable, Name: "User", Table: "users",
		Comment: "A user account.",
		Indexes: []manifest.Index{{Name: "users_email_idx", Columns: []string{"email"}, Unique: true}},
		PK:      manifest.PK{Kind: "single", Columns: []manifest.PKColumn{{Name: "id", GoField: "ID", GoType: "string"}}},
		Features: manifest.Features{
			SoftDelete: &manifest.SoftDeleteFeature{Column: "deleted_at", Type: "timestamp"},
		},
		Columns: []manifest.Column{
			{Name: "id", GoField: "ID", GoType: "string", DBType: "uuid", PK: true, Comment: "primary key"},
			{Name: "email", GoField: "Email", GoType: "string", DBType: "text", Unique: true, Comment: "the login email", Check: "email <> ''", DefaultKind: "none"},
		},
		Relationships: []manifest.Relationship{
			{Name: "posts", Kind: manifest.RelationshipO2M, TargetEntity: "Post", FK: &manifest.FK{Table: "posts", Column: "user_id"}},
			{Name: "roles", Kind: manifest.RelationshipM2M, TargetEntity: "Role", Junction: &manifest.Junction{Table: "user_roles", LocalFK: "user_id", TargetFK: "role_id"}},
		},
		Methods: manifest.Methods{
			Query: []manifest.Method{
				{Name: "Get", Params: []manifest.MethodParam{{Name: "id", Type: "string"}}, Returns: "*User", SQLBodies: map[manifest.Dialect]string{pg: "SELECT * FROM users WHERE id = $1"}},
				{Name: "GetMany", Params: []manifest.MethodParam{{Name: "input", Type: "*GetUsersInput"}}, Returns: "[]*User", SQLBodies: map[manifest.Dialect]string{pg: "SELECT * FROM users WHERE <filter>"}},
			},
			Mutation: []manifest.Method{
				// No SQLBodies — exercises show_sql's -32005 path.
				{Name: "Upsert", Params: []manifest.MethodParam{{Name: "input", Type: "*CreateUserInput"}}, Returns: "*User"},
			},
		},
		Filter:   manifest.Filter{Type: "UserFilter", Fields: []manifest.FilterField{{Name: "Email", Type: "*comparator.String"}}},
		Sort:     manifest.Sort{Type: "UserSort", Fields: []string{"Email", "ID"}},
		Examples: &manifest.Examples{Read: []string{"u, err := c.Users().Get(ctx, id)"}, Write: []string{"u, err := c.Users().Create(ctx, in)"}},
	}
	post := &manifest.Entity{
		Kind: manifest.EntityKindTable, Name: "Post", Table: "posts",
		PK: manifest.PK{Kind: "single", Columns: []manifest.PKColumn{{Name: "id", GoField: "ID", GoType: "string"}}},
		Columns: []manifest.Column{
			{Name: "id", GoField: "ID", GoType: "string", DBType: "uuid", PK: true},
			{Name: "user_id", GoField: "UserID", GoType: "string", DBType: "uuid"},
		},
		Relationships: []manifest.Relationship{
			{Name: "comments", Kind: manifest.RelationshipO2M, TargetEntity: "Comment", FK: &manifest.FK{Table: "comments", Column: "post_id"}},
		},
		Methods: manifest.Methods{
			Query: []manifest.Method{
				{Name: "Get", Params: []manifest.MethodParam{{Name: "id", Type: "string"}}, Returns: "*Post", SQLBodies: map[manifest.Dialect]string{pg: "SELECT * FROM posts WHERE id = $1"}},
			},
		},
		Filter: manifest.Filter{Type: "PostFilter"},
		Sort:   manifest.Sort{Type: "PostSort"},
	}
	comment := &manifest.Entity{
		Kind: manifest.EntityKindTable, Name: "Comment", Table: "comments",
		PK:      manifest.PK{Kind: "single", Columns: []manifest.PKColumn{{Name: "id", GoField: "ID", GoType: "string"}}},
		Columns: []manifest.Column{{Name: "id", GoField: "ID", GoType: "string", DBType: "uuid", PK: true}},
		Relationships: []manifest.Relationship{
			// Back-edge to Post — exercises the join-path cycle guard.
			{Name: "post", Kind: manifest.RelationshipM2O, TargetEntity: "Post", FK: &manifest.FK{Table: "comments", Column: "post_id"}},
		},
		Filter: manifest.Filter{Type: "CommentFilter"},
		Sort:   manifest.Sort{Type: "CommentSort"},
	}
	role := &manifest.Entity{
		Kind: manifest.EntityKindTable, Name: "Role", Table: "roles",
		PK:      manifest.PK{Kind: "single", Columns: []manifest.PKColumn{{Name: "id", GoField: "ID", GoType: "string"}}},
		Columns: []manifest.Column{{Name: "id", GoField: "ID", GoType: "string", DBType: "uuid", PK: true}},
		Relationships: []manifest.Relationship{
			{Name: "users", Kind: manifest.RelationshipM2M, TargetEntity: "User", Junction: &manifest.Junction{Table: "user_roles", LocalFK: "role_id", TargetFK: "user_id"}},
		},
		Filter: manifest.Filter{Type: "RoleFilter"},
		Sort:   manifest.Sort{Type: "RoleSort"},
	}
	membership := &manifest.Entity{
		Kind: manifest.EntityKindTable, Name: "Membership", Table: "memberships",
		PK: manifest.PK{Kind: "composite", Struct: "MembershipPK", Columns: []manifest.PKColumn{
			{Name: "org_id", GoField: "OrgID", GoType: "string"},
			{Name: "user_id", GoField: "UserID", GoType: "string"},
		}},
		Features: manifest.Features{Tenancy: &manifest.TenancyFeature{Column: "org_id", Mode: "required"}},
		Columns: []manifest.Column{
			{Name: "org_id", GoField: "OrgID", GoType: "string", DBType: "uuid", PK: true},
			{Name: "user_id", GoField: "UserID", GoType: "string", DBType: "uuid", PK: true},
		},
		Filter: manifest.Filter{Type: "MembershipFilter"},
		Sort:   manifest.Sort{Type: "MembershipSort"},
	}
	activeUser := &manifest.Entity{
		Kind: manifest.EntityKindView, Name: "ActiveUser", Table: "active_users",
		PK:      manifest.PK{Kind: "none"},
		Columns: []manifest.Column{{Name: "id", GoField: "ID", GoType: "string", DBType: "uuid"}},
		Filter:  manifest.Filter{Type: "ActiveUserFilter"},
		Sort:    manifest.Sort{Type: "ActiveUserSort"},
	}
	// Returned unsorted on purpose — the store sorts by Name on install, and the
	// tools must not depend on input order.
	return []*manifest.Entity{user, post, comment, role, membership, activeUser}
}
